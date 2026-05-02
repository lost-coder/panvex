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

### Terminology — node vs agent vs server (Q-08)

These three words appear all over the code/docs and mean *related but distinct
things*. Consolidated mapping, authoritative for new code:

| Term | Meaning | Where it shows up |
|---|---|---|
| **Node** | The Telemt host being managed — one operator-visible "thing" with a name, a fleet group, telemetry, and clients deployed on it. Conceptual / domain layer. | Specs, plans, UI section headers ("Servers" page renders Nodes). |
| **Agent** | The `panvex-agent` Go process running on the node — the on-host runtime that drives Telemt, dials the panel (or listens), and reports telemetry. | Go code (`cmd/agent`, `internal/agent/...`), DB table `agents`, gRPC `AgentGateway`, HTTP `/api/agents/*`, audit events. **One row in `agents` ⇄ one node.** |
| **Server** | UI label for what the operator perceives as "the box". Synonymous with Node in user-facing copy. | React dashboard pages: `ServersPage`, `ServerDetailPage`, `AddServerContainer`. |

When writing new code:

- **Database, Go types, HTTP/gRPC routes** → use `agent` / `agents` / `agent_id`.
  This is the canonical machine-side identifier; do not introduce a `nodes`
  table or `node_id` column.
- **User-facing UI copy** → use "Server" (existing convention). "Node" is
  acceptable in lower-level technical labels (e.g. fleet detail page).
- **Specs / planning docs** → prefer "agent" so the plan's tokens map 1-to-1
  onto the schema. If the doc reaches for "node" for readability, add an
  explicit "node ≡ agent in DB" note (see
  `docs/superpowers/specs/2026-04-29-reverse-connection-mode-design.md`).

The `node_name` column on `agents` is the operator-supplied display name —
that is the one place where "node" survives in the schema, intentionally.

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
web/                 React dashboard (src/ui/ is the inlined panvex-ui kit — Phase 4)
deploy/              Docker Compose, nginx, install scripts
.githooks/pre-push   Mirrors CI: golangci-lint + race tests + govulncheck +
                     frontend build/lint/audit. Bypass: PANVEX_SKIP_PREPUSH=1.
```

Workspace-level dirs live one level up: `../scripts/` (dev fleet), `../.tmp/`
(dev DB, logs, plans). See the workspace CLAUDE.md for that context.

## Reverse connection mode

Each agent has a `transport_mode` field in the `agents` table (`inbound` or
`outbound`). In `inbound` mode the agent dials the panel (default). In
`outbound` (reverse) mode the panel dials an agent that is already listening.
To enroll an outbound agent: call `POST /api/agents/{id}/install-command`
to get a `curl ... | bash` one-liner with a short-lived bootstrap token; the
agent starts with `--mode=reverse`, sends a CSR via the `EnrollOutbound` gRPC
call, and the panel signs and returns the cert. Switching an existing agent is
done via `PUT /api/agents/{id}/transport-mode`, which persists the new mode,
enqueues a `switch_transport_mode` job, and notifies `agenttransport.Manager`
to spawn or tear down outbound supervisors. Key packages:
`internal/controlplane/agenttransport` (panel transport manager + supervisors),
`internal/controlplane/bootstrap` (token issuance, enroll driver, install
handler), `internal/agent/transport` (agent dial/listen).

Certificate renewal works in both dial and listen modes via the Connect
bidi-stream: the agent sends `RenewalRequest{agent_id, csr_pem}` when its cert
enters the renewal window, and the panel replies with `RenewalResponse` carrying
the signed cert. The agent generates a fresh ECDSA P-256 keypair locally, sends
only the CSR, and validates the returned cert pairs with the new key before
persisting. The dial-mode outer pre-connection `RenewCertificate` unary RPC is
retained as a fallback but is skipped in listen mode.

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
docker compose -f deploy/docker-compose.postgres.yml up --build -d  # dev — default creds
POSTGRES_PASSWORD=... PANVEX_ENCRYPTION_KEY=... \
  docker compose -f deploy/docker-compose.prod.yml up --build -d  # prod — TLS, no defaults

# Bootstrap first admin (SQLite default)
go run ./cmd/control-plane bootstrap-admin -username admin -password '<pw>'
```
