# Each FROM line is digest-pinned (Phase-3 §3.3): the `tag@sha256:...`
# form locks the image to an immutable manifest, so a registry
# republish of `node:22-alpine` cannot silently change what we build.
# The Dependabot docker rule (.github/dependabot.yml) bumps both the
# tag and the digest on a weekly cadence — operators rebase their
# release branch onto the dependabot PR before tagging.
#
# Refresh manually with:
#   docker manifest inspect <image>:<tag> | jq -r '.manifests[0].digest // .config.digest'
# (or `docker buildx imagetools inspect <image>:<tag>` once docker is
# new enough on the operator's box).

FROM node:25-alpine@sha256:bdf2cca6fe3dabd014ea60163eca3f0f7015fbd5c7ee1b0e9ccb4ced6eb02ef4 AS web-builder
WORKDIR /src/web

COPY web/package*.json ./
RUN npm ci

COPY web ./
COPY cmd/control-plane /src/cmd/control-plane
RUN npm run build:embed

FROM golang:1.26-alpine@sha256:f85330846cde1e57ca9ec309382da3b8e6ae3ab943d2739500e08c86393a21b1 AS control-plane-builder
WORKDIR /src

# modernc.org/sqlite is pure-Go, so we build with CGO disabled — drops
# the libc dependency, shrinks the binary by ~30%, and lets us skip
# the `build-base` apk install that older revisions needed.
ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY proto ./proto
COPY db ./db
# -ldflags="-s -w"  strip symbol + DWARF tables — saves ~25% binary size.
# -trimpath          remove $GOPATH absolute paths from the binary so
#                    panic stacks/build IDs stay reproducible across
#                    builders and don't leak host filesystem layout.
RUN go build -ldflags="-s -w" -trimpath -o /out/panvex-control-plane ./cmd/control-plane

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS control-plane
WORKDIR /app

RUN apk add --no-cache ca-certificates && \
    addgroup -S panvex && adduser -S panvex -G panvex

COPY --from=control-plane-builder /out/panvex-control-plane ./panvex-control-plane

USER panvex

EXPOSE 8080 8443

# Liveness probe: /healthz is always registered on the control-plane
# router and returns 200 once the HTTP listener is up (see internal/
# controlplane/server/server.go). BusyBox wget is available by default
# in alpine, so no extra package is needed. start-period gives the
# process time to bind, run migrations, and load state; retries smooths
# over a single GC pause or transient DB blip without flapping the
# container.
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:8080/healthz >/dev/null 2>&1 || exit 1

ENTRYPOINT ["./panvex-control-plane"]

FROM nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de AS web

# BP-Medium: switch the nginx stage from the default root-PID-1 entrypoint
# to running as the built-in unprivileged `nginx` user (UID 101). The
# upstream image starts as root only to bind :80 before dropping to the
# `nginx` worker — we don't need that, our default.conf binds a high port
# (:8080) so no NET_BIND_SERVICE capability is required and the container
# can run with `runAsNonRoot: true` under PodSecurity `restricted`.
#
# The default.conf we ship is rewritten to `listen 8080;` to match. The
# Helm chart's service.httpPort is set to 8080 to match the container port.
COPY deploy/nginx/default.conf /etc/nginx/conf.d/default.conf
COPY --from=web-builder /src/cmd/control-plane/.embedded-ui /usr/share/nginx/html

# nginx writes pid/access/error logs and creates several runtime
# directories (/var/cache/nginx, /var/run) at startup. The base image
# leaves these owned by root; chown them to the unprivileged `nginx`
# user so the worker can write without an explicit volume mount, and
# move the pid file out of /var/run/ (which is symlinked to /run/ and
# is root-owned on alpine).
RUN sed -i 's|listen 80;|listen 8080;|' /etc/nginx/conf.d/default.conf && \
    sed -i 's|^pid .*|pid /tmp/nginx.pid;|' /etc/nginx/nginx.conf && \
    chown -R nginx:nginx /usr/share/nginx/html /var/cache/nginx /etc/nginx/conf.d && \
    touch /tmp/nginx.pid && chown nginx:nginx /tmp/nginx.pid

USER nginx

EXPOSE 8080

# nginx is a pass-through static/proxy layer. We probe the local nginx
# port so the container reports unhealthy when the worker has crashed
# even though the backend may still be reachable from another path.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:8080/ >/dev/null 2>&1 || exit 1
