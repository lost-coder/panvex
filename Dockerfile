FROM node:22-alpine AS web-builder
WORKDIR /src/web

COPY web/package*.json ./
RUN npm ci

COPY web ./
COPY cmd/control-plane /src/cmd/control-plane
RUN npm run build:embed

FROM golang:1.26-alpine AS control-plane-builder
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

FROM alpine:3.22 AS control-plane
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

FROM nginx:1.29-alpine AS web

COPY deploy/nginx/default.conf /etc/nginx/conf.d/default.conf
COPY --from=web-builder /src/cmd/control-plane/.embedded-ui /usr/share/nginx/html

EXPOSE 80

# nginx is a pass-through static/proxy layer. We probe the local nginx
# port so the container reports unhealthy when the worker has crashed
# even though the backend may still be reachable from another path.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:80/ >/dev/null 2>&1 || exit 1
