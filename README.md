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
- local working plans belong in `.tmp/plans/` and stay out of git.

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

This is the split development workflow with a standalone Go backend and a Vite development server.

You do not need prebuilt frontend assets for:

- `go build ./...`
- `go run ./cmd/control-plane`

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

## Single-binary release workflow

1. Build the embedded frontend assets:

   ```sh
   cd web
   npm install
   npm run build:embed
   ```

2. Build the control-plane binary with the embedded UI release tag:

   ```sh
   go build -tags embeddedui -o panvex-control-plane ./cmd/control-plane
   ```

3. Bootstrap the first admin:

   ```sh
   ./panvex-control-plane bootstrap-admin -username admin -password '<strong-password>'
   ```

   The bootstrap user starts with TOTP disabled by default.

4. Start the single-binary release:

   ```sh
   ./panvex-control-plane -http-addr :8080 -grpc-addr :8443
   ```

   The dashboard is served by the same binary on `http://127.0.0.1:8080`.

## Optional account TOTP

Panvex keeps TOTP optional by default for local accounts, including the first admin.

- Sign in with username and password when TOTP is disabled.
- Open `Settings` and use `Optional two-factor authentication` to start setup.
- Confirm setup with the current password and a fresh code from the authenticator app before TOTP becomes active.
- Disable active TOTP with the current password and current TOTP code.

For multi-user installs, admins can open `Settings` and use `Admin TOTP recovery` to reset another user's TOTP from the web panel.

For emergency recovery on the server, reset a local user's TOTP through the control-plane CLI:

```sh
./panvex-control-plane reset-user-totp \
  -username admin
```

If the control-plane is using PostgreSQL instead of the default SQLite backend, pass the storage flags explicitly:

```sh
./panvex-control-plane reset-user-totp \
  -storage-driver postgres \
  -storage-dsn 'postgres://panvex:password@127.0.0.1:5432/panvex?sslmode=disable' \
  -username admin
```

## Agent quick start

1. Create an enrollment token from the dashboard Settings screen.
2. On the Linux server that runs Telemt, install the agent:

   ```sh
   curl -fsSL https://github.com/panvex/panvex/releases/latest/download/install-agent.sh | \
     sudo sh -s -- \
       --panel-url https://panel.example.com \
       --enrollment-token "<token>"
   ```

   The installer downloads the agent, asks for the local Telemt API settings, bootstraps the agent identity through the panel HTTPS API, and starts a `systemd` service.

3. For an advanced manual flow, bootstrap a downloaded binary directly without saving any `ca_pem` file:

   ```sh
   ./panvex-agent bootstrap \
     -panel-url https://panel.example.com \
     -enrollment-token "<token>" \
     -state-file /var/lib/panvex-agent/agent-state.json
   ```

## Verification

- `go build ./...`
- `npm run build` from `web`
- `npm run build:embed` from `web`
- `go build -tags embeddedui ./cmd/control-plane`
- `bash deploy/install.sh --help`
- `bash deploy/install-agent.sh --help`
- `go test ./cmd/agent ./internal/agent/state -v`
- `rg -n "services:|sqlite|postgres|web:" deploy/docker-compose.sqlite.yml deploy/docker-compose.postgres.yml Dockerfile`
- `go test ./cmd/control-plane -run "TestRunBootstrapAdmin|TestRunResetUserTotp" -v`
- `go test ./internal/controlplane/auth ./internal/controlplane/server -run "TestServiceBootstrapUserLeavesTotpDisabledByDefault|TestServiceAuthenticateAllowsOperatorWithoutTotpWhenDisabled|TestServiceEnableTotpRequiresPendingSetup|TestServiceEnableTotpRequiresValidPasswordAndCode|TestServiceDisableTotpRequiresValidPasswordAndCode|TestServiceResetTotpClearsEnabledState|TestHTTPAuthTotpSetupEnableDisableFlow|TestHTTPUsersTotpResetRequiresAdminAndClearsTarget" -v`
- `go test ./internal/controlplane/auth ./internal/controlplane/jobs ./internal/controlplane/server ./internal/controlplane/state ./internal/controlplane/storage/migrate ./internal/controlplane/storage/postgres ./internal/controlplane/storage/sqlite -v`

On the current Windows environment, `go test` for `internal/controlplane/config`, `internal/controlplane/presence`, and `internal/controlplane/storage/storagetest` must be executed through compiled test binaries because temporary `.test.exe` launches are denied:

```powershell
New-Item -ItemType Directory -Force .tmp/tests | Out-Null
go test -c -o .tmp/tests/config.test.exe ./internal/controlplane/config
& .\.tmp\tests\config.test.exe --% -test.v
go test -c -o .tmp/tests/presence.test.exe ./internal/controlplane/presence
& .\.tmp\tests\presence.test.exe --% -test.v
go test -c -o .tmp/tests/storagetest.test.exe ./internal/controlplane/storage/storagetest
& .\.tmp\tests\storagetest.test.exe --% -test.v
```
