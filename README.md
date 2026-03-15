# Panvex

Panvex is a control-plane and web dashboard for managing multiple Telemt nodes.

## Repository layout

- `cmd/control-plane` runs the operator HTTP API and the agent gRPC gateway.
- `cmd/agent` runs a local agent that talks to Telemt over loopback only.
- `internal/controlplane/...` contains auth, jobs, presence, state, and server logic.
- `internal/agent/...` contains Telemt client, runtime orchestration, and agent state helpers.
- `internal/gatewayrpc` contains the shared gRPC transport contract used by the control-plane and the agent.
- `web` contains the React dashboard.
- `db/migrations` and `db/queries` contain the initial PostgreSQL schema and sqlc query set.
- `proto` contains the human-readable gateway contract.

## Release installer

For a single-binary release install, use the release installer:

```sh
curl -fsSL https://github.com/panvex/panvex/releases/latest/download/install.sh | sh
```

The installer defaults to SQLite.
When run as root it installs the binary into `/usr/local/bin`, writes runtime files into `/etc/panvex` and `/var/lib/panvex`, and installs a disabled-but-enabled systemd unit.
When run without root it installs under `~/.local` and writes a local start script instead of a systemd unit.

To install against PostgreSQL instead:

```sh
PANVEX_STORAGE_DRIVER=postgres \
PANVEX_STORAGE_DSN='postgres://panvex:password@127.0.0.1:5432/panvex?sslmode=disable' \
curl -fsSL https://github.com/panvex/panvex/releases/latest/download/install.sh | sh
```

Installer help is available through:

```sh
bash deploy/install.sh --help
```

## Docker deployment

For a split Docker deployment with SQLite:

```sh
docker compose -f deploy/docker-compose.sqlite.yml up --build -d
docker compose -f deploy/docker-compose.sqlite.yml exec backend \
  ./panvex-control-plane bootstrap-admin \
  -storage-driver sqlite \
  -storage-dsn /var/lib/panvex/panvex.db \
  -username admin \
  -password '<strong-password>'
```

The dashboard is then available on `http://127.0.0.1:8080`, while the agent gRPC gateway is exposed on `127.0.0.1:8443`.

For a split Docker deployment with PostgreSQL:

```sh
docker compose -f deploy/docker-compose.postgres.yml up --build -d
docker compose -f deploy/docker-compose.postgres.yml exec backend \
  ./panvex-control-plane bootstrap-admin \
  -storage-driver postgres \
  -storage-dsn 'postgres://panvex:panvex@postgres:5432/panvex?sslmode=disable' \
  -username admin \
  -password '<strong-password>'
```

The SQLite compose file keeps SQLite as the default storage mode.
The PostgreSQL compose file introduces PostgreSQL explicitly and does not change the SQLite default path.

## Control-plane quick start

1. Bootstrap the first local admin:

   ```powershell
   go run ./cmd/control-plane bootstrap-admin `
     -username admin `
     -password "<strong-password>"
   ```

   By default this writes the first admin into the SQLite database at `data/panvex.db`.
   To target PostgreSQL instead:

   ```powershell
   go run ./cmd/control-plane bootstrap-admin `
     -storage-driver postgres `
     -storage-dsn "postgres://panvex:password@127.0.0.1:5432/panvex?sslmode=disable" `
     -username admin `
     -password "<strong-password>"
   ```

2. Start the control-plane:

   ```powershell
   go run ./cmd/control-plane -http-addr :8080 -grpc-addr :8443
   ```

   The default startup backend is SQLite at `data/panvex.db`.
   To start against PostgreSQL:

   ```powershell
   go run ./cmd/control-plane `
     -http-addr :8080 `
     -grpc-addr :8443 `
     -storage-driver postgres `
     -storage-dsn "postgres://panvex:password@127.0.0.1:5432/panvex?sslmode=disable"
   ```

3. Start the web dashboard:

   ```powershell
   cd web
   npm install
   npm run dev
   ```

## Agent quick start

1. Create an enrollment token from the dashboard Settings screen and save the returned `ca_pem` to a file.
2. Start the agent:

   ```powershell
   go run ./cmd/agent `
     -gateway-addr 127.0.0.1:8443 `
     -gateway-server-name control-plane.panvex.internal `
     -ca-file control-plane-ca.pem `
     -enrollment-token "<token>" `
     -state-file data/agent-state.json `
     -telemt-url http://127.0.0.1:8080 `
     -telemt-auth "<telemt-auth>"
   ```

## Verification

- `go build ./...`
- `npm run build` from `web`
- Package-level Go tests are executed through compiled test binaries because the current Windows environment rejects default `go test` temp executables.
