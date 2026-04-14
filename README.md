<h1 align="center">
  <br>
  <img src="https://img.shields.io/badge/Panvex-Control%20Plane-0ea5e9?style=for-the-badge&logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIyNCIgaGVpZ2h0PSIyNCIgdmlld0JveD0iMCAwIDI0IDI0IiBmaWxsPSJub25lIiBzdHJva2U9IndoaXRlIiBzdHJva2Utd2lkdGg9IjIiPjxwYXRoIGQ9Ik0yMiAxMmgtNGwtMyA5TDkgM2wtMyA5SDIiLz48L3N2Zz4=&logoColor=white" alt="Panvex" />
  <br>
</h1>

<p align="center">
  <strong>Fleet management control plane for Telemt MTProto proxy nodes</strong>
</p>

<p align="center">
  <a href="#-quick-install">Quick Install</a> &nbsp;&bull;&nbsp;
  <a href="#-features">Features</a> &nbsp;&bull;&nbsp;
  <a href="#%EF%B8%8F-architecture">Architecture</a> &nbsp;&bull;&nbsp;
  <a href="#-development">Development</a> &nbsp;&bull;&nbsp;
  <a href="#-docker">Docker</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black" alt="React" />
  <img src="https://img.shields.io/badge/gRPC-mTLS-4285F4?logo=google&logoColor=white" alt="gRPC" />
  <img src="https://img.shields.io/badge/Linux-only-FCC624?logo=linux&logoColor=black" alt="Linux" />
</p>

---

## ✨ Features

| | Feature | Description |
|---|---------|-------------|
| 📊 | **Fleet Dashboard** | Real-time monitoring with metrics, health indicators, and alerts |
| 👥 | **Managed Clients** | Centralized client management with secret rotation and quotas |
| 🤖 | **Agent System** | Lightweight per-node agents with mTLS enrollment and gRPC streaming |
| 🗄️ | **Dual Storage** | SQLite for dev/lightweight, PostgreSQL for production |
| 🔄 | **Self-Update** | Panel and agents update themselves from GitHub Releases |
| 📦 | **Embedded UI** | Single binary ships the React dashboard — no separate web server |
| 🔐 | **TOTP 2FA** | Optional two-factor authentication for operator accounts |
| 🛡️ | **RBAC** | Viewer, Operator, and Admin roles with middleware enforcement |

---

## 🚀 Quick Install

### Control Plane

```sh
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/lost-coder/panvex/main/deploy/install.sh)"
```

> Interactive wizard: ports, storage, TLS, firewall, admin account — all configured step by step.

### Agent

```sh
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/lost-coder/panvex/main/deploy/install-agent.sh)"
```

> Requires a panel URL and enrollment token (create one in **Settings → Enrollment Tokens**).

<details>
<summary>📋 Non-interactive mode (CI / automation)</summary>

```sh
# Control Plane
PANVEX_ADMIN_PASS='<password>' \
PANVEX_HTTP_PORT=8080 \
PANVEX_GRPC_PORT=8443 \
  sudo -E bash install.sh

# Agent
PANVEX_PANEL_URL='https://panel.example.com' \
PANVEX_ENROLLMENT_TOKEN='<token>' \
  sudo -E bash install-agent.sh
```

Run `bash install.sh --help` for all environment variables.

</details>

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────┐
│                    🌐 Browser                        │
│           React · TanStack Router/Query              │
├─────────────────────────────────────────────────────┤
│              📡 Control Plane (:8080)                │
│        HTTP API · WebSocket · Embedded UI            │
├─────────────────────────────────────────────────────┤
│              🔒 gRPC Gateway (:8443)                 │
│         mTLS · Bidirectional Stream · Jobs           │
├─────────────────────────────────────────────────────┤
│              🤖 Agent (per Telemt node)              │
│       Heartbeats · Snapshots · Job Execution         │
└─────────────────────────────────────────────────────┘
```

<details>
<summary>📁 Repository Layout</summary>

| Directory | Description |
|-----------|-------------|
| `cmd/control-plane` | Control plane server (HTTP + gRPC + embedded UI) |
| `cmd/agent` | Agent binary with bootstrap and enrollment |
| `internal/controlplane` | Auth, jobs, presence, storage, server logic |
| `internal/agent` | Telemt client, runtime, self-updater |
| `internal/gatewayrpc` | Generated gRPC stubs (protobuf) |
| `internal/security` | Enrollment, crypto, mTLS CA |
| `web` | React dashboard (Vite + TailwindCSS 4 + TanStack) |
| `db/migrations` | PostgreSQL and SQLite schema migrations |
| `proto` | Protobuf gateway contract |
| `deploy` | Install scripts, Docker Compose, nginx config |

</details>

<details>
<summary>🔧 Tech Stack</summary>

| Layer | Technology |
|-------|------------|
| Backend | Go 1.26, chi/v5, pgx/v5, modernc.org/sqlite, gRPC |
| Frontend | React 19, Vite 7, TailwindCSS 4, TanStack Router + Query |
| UI Kit | [@lost-coder/panvex-ui](https://github.com/lost-coder/panvex-ui) — Radix UI + CVA |
| Database | PostgreSQL (primary) · SQLite (lightweight) |
| Deploy | Multi-stage Docker · systemd · nginx |

</details>

---

## 💻 Development

### Prerequisites

- **Go** 1.26+ &nbsp;·&nbsp; **Node.js** 22+ &nbsp;·&nbsp; [sqlc](https://sqlc.dev) &nbsp;·&nbsp; [protoc](https://grpc.io/docs/protoc-installation/) + Go plugins

### Backend

```sh
go build ./...                    # Build all
go test ./...                     # Run tests
go test -race ./...               # Race detector
golangci-lint run ./...           # Lint
sqlc generate                     # Regenerate DB code
```

### Frontend

```sh
cd web
npm install                       # Install deps
npm run dev                       # Dev server (proxies API to :8080)
npm run build                     # Production build
npm run lint                      # ESLint
```

### 🏃 Local Development Flow

**1.** Bootstrap admin:

```sh
go run ./cmd/control-plane bootstrap-admin \
  -username admin \
  -password '<strong-password>'
```

**2.** Start control plane:

```sh
go run ./cmd/control-plane -http-addr :8080 -grpc-addr :8443
```

**3.** Start frontend dev server:

```sh
cd web && npm run dev
```

> Dashboard at `http://localhost:5173`, API proxied to `:8080`

<details>
<summary>📦 Single binary build</summary>

```sh
cd web && npm run build:embed
cd .. && go build -tags embeddedui -o panvex-control-plane ./cmd/control-plane
```

</details>

---

## 🐳 Docker

<details>
<summary><strong>SQLite</strong> (lightweight)</summary>

```sh
docker compose -f deploy/docker-compose.sqlite.yml up --build -d

docker compose -f deploy/docker-compose.sqlite.yml exec backend \
  ./panvex-control-plane bootstrap-admin \
  -storage-driver sqlite \
  -storage-dsn /var/lib/panvex/panvex.db \
  -username admin \
  -password '<strong-password>'
```

</details>

<details>
<summary><strong>PostgreSQL</strong> (production)</summary>

```sh
docker compose -f deploy/docker-compose.postgres.yml up --build -d

docker compose -f deploy/docker-compose.postgres.yml exec backend \
  ./panvex-control-plane bootstrap-admin \
  -storage-driver postgres \
  -storage-dsn 'postgres://panvex:panvex@postgres:5432/panvex?sslmode=disable' \
  -username admin \
  -password '<strong-password>'
```

</details>

> Dashboard: `http://localhost:8080` &nbsp;·&nbsp; gRPC: `localhost:8443`

---

## 🤖 Agent Deployment

1. Create an enrollment token: **Settings → Enrollment Tokens**
2. On each Telemt server:

```sh
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/lost-coder/panvex/main/deploy/install-agent.sh)"
```

<details>
<summary>Manual bootstrap (without installer)</summary>

```sh
./panvex-agent bootstrap \
  -panel-url https://panel.example.com \
  -enrollment-token '<token>' \
  -state-file /var/lib/panvex-agent/agent-state.json
```

</details>

---

## 👥 Managed Clients

Create and manage Telemt clients centrally from the dashboard:

- 🔑 Generate secrets and `user_ad_tag`
- 📏 Set limits: connections, unique IPs, quota, expiration
- 🌐 Assign by fleet group or individual nodes
- 🔄 Rotate secrets without recreating the client
- 📈 Live deployment status, connection links, and usage per node

---

## 🔐 Security

**Two-Factor Authentication** — TOTP 2FA is optional. Enable in Profile page.

Emergency TOTP reset via CLI:

```sh
./panvex-control-plane reset-user-totp \
  -storage-driver sqlite \
  -storage-dsn /var/lib/panvex/panvex.db \
  -username admin
```

---

## 🔄 Updates

The control plane checks GitHub Releases for new versions automatically.

| Method | Command |
|--------|---------|
| **Dashboard** | Settings → Updates → *Update Panel* / *Update Agent* |
| **CLI** | `./panvex-control-plane self-update` |
| **Auto-update** | Enable in Settings → Updates (disabled by default) |

Agents can be updated individually or in bulk. The panel sends an update job via gRPC — the agent downloads and installs the new binary automatically.

---

<p align="center">
  <sub>Built with ❤️ for Telemt fleet operators</sub>
</p>
