# Panvex

Control-plane + web dashboard + agent for managing fleets of Telemt nodes.

## Domain context

Telemt is an MTProxy server for Telegram (Rust + Tokio). It proxies Telegram
traffic using the MTProto protocol with obfuscation modes: Classic, Secure (dd
prefix), and Fake TLS (ee prefix + SNI fronting).

Panvex manages multiple Telemt instances centrally:
- the control-plane exposes an HTTP API and a gRPC gateway
- each agent runs on a server alongside Telemt, connects to the control-plane
  over gRPC, and drives Telemt via its local loopback API
- the web dashboard lets operators manage clients, nodes, fleet groups,
  enrollment tokens, and jobs

Key domain entities: Node, Client, FleetGroup, EnrollmentToken, Job, Presence.

A "client" in Panvex maps to a Telemt proxy client: it has a secret, limits
(max_tcp_conns, max_unique_ips, quota, expiry), and is assigned to nodes by
fleet group or explicitly. After each save, Panvex queues rollout jobs; each
node returns its own Telegram connection link.

## Repository layout

```
cmd/control-plane/   HTTP :8080 + gRPC :8443 gateway; embeds web UI via go:embed
cmd/agent/           Agent binary: bootstrap/enrollment + Telemt API integration
internal/
  controlplane/      auth, jobs, presence, state, server (HTTP handlers)
  agent/             Telemt client, runtime orchestration, agent state
  gatewayrpc/        shared gRPC transport contract (control-plane <-> agent)
  dbsqlc/            sqlc-generated DB layer — DO NOT edit manually
db/
  migrations/        PostgreSQL schema
  queries/           SQL source for sqlc
proto/               Protobuf definitions for gRPC gateway
web/                 React dashboard
deploy/              Docker Compose, nginx, install scripts
.tmp/plans/          Local working plans (gitignored)
```

## Tech stack

- **Go 1.26**: chi/v5 router, pgx/v5, modernc.org/sqlite, gRPC, WebSocket
- **React 19**: Vite 7, TailwindCSS 4, TanStack Router + Query, Radix UI, Recharts
- **Storage**: PostgreSQL (primary) or SQLite (lightweight), managed via sqlc
- **Deploy**: multi-stage Docker, nginx reverse proxy

## Key conventions

- `internal/dbsqlc/` is auto-generated — edit `db/queries/*.sql`, then run
  `sqlc generate`
- Frontend embeds into the binary via `npm run build:embed` ->
  `cmd/control-plane/.embedded-ui/`; build tag `embeddedui` activates it
- `config.toml` and `proxy-secret` are local runtime files, not committed
- Storage driver is selected at startup via `-storage-driver sqlite|postgres`
  and `-storage-dsn <dsn>`

## Commands

```bash
# Backend
go build ./...
go test ./...
go test ./internal/controlplane/auth ./internal/controlplane/jobs \
        ./internal/controlplane/server ./internal/controlplane/state \
        ./internal/controlplane/storage/... -v

# Frontend
cd web && npm install
cd web && npm run dev          # Vite dev server
cd web && npm run build        # type-check + production build
cd web && npm run build:embed  # build into cmd/control-plane/.embedded-ui

# sqlc
sqlc generate

# Docker
docker compose -f deploy/docker-compose.sqlite.yml up --build -d
docker compose -f deploy/docker-compose.postgres.yml up --build -d

# Bootstrap first admin (SQLite default)
go run ./cmd/control-plane bootstrap-admin -username admin -password '<pw>'
```
