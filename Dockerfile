# Each FROM line is digest-pinned (Phase-3 §3.3): the `tag@sha256:...`
# form locks the image to an immutable manifest, so a registry
# republish of `node:26-alpine` cannot silently change what we build.
# The Dependabot docker rule (.github/dependabot.yml) bumps both the
# tag and the digest on a weekly cadence — operators rebase their
# release branch onto the dependabot PR before tagging.
#
# Refresh manually with:
#   docker manifest inspect <image>:<tag> | jq -r '.manifests[0].digest // .config.digest'
# (or `docker buildx imagetools inspect <image>:<tag>` once docker is
# new enough on the operator's box).

FROM node:26-alpine@sha256:725aeba2364a9b16beae49e180d83bd597dbd0b15c47f1f28875c290bfd255b9 AS web-builder
WORKDIR /src/web

# .npmrc carries `legacy-peer-deps=true` — eslint-plugin-jsx-a11y@6.10.2
# still declares a peer range of eslint up to ^9 while this project
# tracks eslint@10. Without copying it here, the image build's `npm ci`
# re-resolves peers strictly and fails with ERESOLVE (the host E2E job
# works only because it runs inside web/ where .npmrc is present).
COPY web/package*.json web/.npmrc ./
RUN npm ci

COPY web ./
COPY cmd/control-plane /src/cmd/control-plane
RUN npm run build:embed

FROM golang:1.26-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS control-plane-builder
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
# internal/controlplane/server/openapi_adapter.go imports the generated
# github.com/lost-coder/panvex/openapi package, so its source must be in
# the build context alongside cmd/internal/proto/db.
COPY openapi ./openapi
# deploy/install-agent.sh is the canonical bash installer. //go:embed in
# internal/controlplane/server/install_script.go cannot reference paths
# outside the package, so we mirror the file into the package via
# `go generate` before the build. The mirror is .gitignored; this step
# also runs in the Makefile, pre-push hook, and CI.
COPY deploy/install-agent.sh ./deploy/install-agent.sh
RUN go generate ./internal/controlplane/server/...
# -ldflags="-s -w"  strip symbol + DWARF tables — saves ~25% binary size.
# -trimpath          remove $GOPATH absolute paths from the binary so
#                    panic stacks/build IDs stay reproducible across
#                    builders and don't leak host filesystem layout.
RUN go build -ldflags="-s -w" -trimpath -o /out/panvex-control-plane ./cmd/control-plane

# SBOM stage: produce a CycloneDX JSON manifest of every Go module that
# made it into the binary. Anchore syft reads the build artefact
# directly — no source-tree round-trip — so the SBOM matches what the
# operator actually ships. The output is copied into the final image at
# /sbom/control-plane.cdx.json so cluster scanners (Trivy, Grype) can
# read it without re-deriving from go.sum. Release archives already
# carry their own SBOM via release.yml; this entry covers the image
# distribution path.
# anchore/syft is digest-pinned like every other base image (O7).
# Dependabot's docker rule (.github/dependabot.yml, /) keeps both the
# tag and the digest current. To refresh the digest manually:
#     docker manifest inspect anchore/syft:<tag> \
#       | jq -r '.manifests[0].digest // .config.digest'
# and update the tag + @sha256 below together.
# The anchore/syft image ships only the static `syft` binary with no
# shell — a shell-form RUN inside it fails with `exec: "/bin/sh": no such
# file or directory`. Copy the pinned syft binary into an alpine stage
# (busybox sh/test/head/grep) and run it there instead.
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS sbom-builder
COPY --from=anchore/syft:v1.45.1@sha256:c6d5719f48f5a5986acf2847eb1ed7c53176e712d5721fcd156184cfb262f6eb /syft /usr/local/bin/syft
COPY --from=control-plane-builder /out/panvex-control-plane /panvex-control-plane
RUN syft /panvex-control-plane -o cyclonedx-json=/sbom/control-plane.cdx.json && \
    # Defensive assert: a future syft major that changes the -o flag
    # semantics could exit zero with an empty file, leaving the final
    # image carrying a useless SBOM. Fail the build instead.
    test -s /sbom/control-plane.cdx.json && \
    head -c1 /sbom/control-plane.cdx.json | grep -q '{'

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS control-plane
WORKDIR /app

# OCI image labels — operator scanners look these up to attribute the
# image back to the project, the SBOM file, and the source repo.
LABEL org.opencontainers.image.title="panvex-control-plane" \
      org.opencontainers.image.source="https://github.com/lost-coder/panvex" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.description="Panvex control plane (HTTP + gRPC + embedded UI)." \
      org.opencontainers.image.sbom="/sbom/control-plane.cdx.json"

# `apk upgrade` pulls patched OS packages (openssl/libxml2 et al.) from the
# alpine repo: the pinned base digest lags behind package fixes, so without
# this the Trivy HIGH,CRITICAL gate flags fixed-but-not-yet-rebased CVEs
# (e.g. CVE-2026-45447 openssl, CVE-2026-6732 libxml2). Dependabot bumps the
# base digest on its own cadence; this keeps the shipped image patched in
# between.
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates && \
    addgroup -S panvex && adduser -S -G panvex -H -u 10001 panvex

COPY --from=control-plane-builder /out/panvex-control-plane ./panvex-control-plane
COPY --from=sbom-builder /sbom/control-plane.cdx.json /sbom/control-plane.cdx.json

# /var/lib/panvex is where the control-plane writes all local state: the
# SQLite DB (PANVEX_STORAGE_DSN=/var/lib/panvex/panvex.db, see
# deploy/docker-compose.sqlite.yml), the geoip DB
# (internal/controlplane/server/geoip_settings.go), and backup/restore
# archives (cmd/control-plane/backup.go). Create it and hand ownership to
# the unprivileged `panvex` user before dropping root, so both a bind mount
# and a named volume (which Docker seeds from the image's directory
# contents/ownership on first use) are writable post-USER.
RUN mkdir -p /var/lib/panvex && chown -R panvex:panvex /var/lib/panvex

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

FROM nginx:1.31-alpine@sha256:54f2a904c251d5a34adf545a72d32515a15e08418dae0266e23be2e18c66fefa AS web

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
# apk upgrade patches OS packages (openssl/libxml2 et al.) the pinned
# nginx base digest still ships vulnerable — this is the image the Trivy
# HIGH,CRITICAL gate actually scans (default build target = last stage).
RUN apk upgrade --no-cache && \
    sed -i 's|listen 80;|listen 8080;|' /etc/nginx/conf.d/default.conf && \
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
