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

## Control-plane quick start

1. Bootstrap the first local admin:

   ```powershell
   go run ./cmd/control-plane bootstrap-admin `
     -state-file data/auth-state.json `
     -username admin `
     -password "<strong-password>"
   ```

2. Start the control-plane:

   ```powershell
   go run ./cmd/control-plane -http-addr :8080 -grpc-addr :8443 -state-file data/auth-state.json
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
