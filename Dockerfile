FROM node:22-alpine AS web-builder
WORKDIR /src/web

COPY web/package*.json ./
RUN npm ci

COPY web ./
COPY cmd/control-plane /src/cmd/control-plane
RUN npm run build:embed

FROM golang:1.26-alpine AS control-plane-builder
WORKDIR /src

RUN apk add --no-cache build-base

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY proto ./proto
COPY db ./db
RUN go build -o /out/panvex-control-plane ./cmd/control-plane

FROM alpine:3.22 AS control-plane
WORKDIR /app

RUN apk add --no-cache ca-certificates && \
    addgroup -S panvex && adduser -S panvex -G panvex

COPY --from=control-plane-builder /out/panvex-control-plane ./panvex-control-plane

USER panvex

EXPOSE 8080 8443

ENTRYPOINT ["./panvex-control-plane"]

FROM nginx:1.29-alpine AS web

COPY deploy/nginx/default.conf /etc/nginx/conf.d/default.conf
COPY --from=web-builder /src/cmd/control-plane/.embedded-ui /usr/share/nginx/html

EXPOSE 80
