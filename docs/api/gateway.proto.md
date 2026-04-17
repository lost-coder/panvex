# AgentGateway gRPC reference

Source: [`proto/agent_gateway.proto`](../../proto/agent_gateway.proto)
Package: `panvex.gateway.v1`
Go option: `go_package = "github.com/lost-coder/panvex/internal/gatewayrpc"`

The `AgentGateway` service is the single gRPC contract between the Panvex
control-plane and its per-node agents. It carries enrollment-time certificate
renewal and, once the agent is bootstrapped, a long-lived bidirectional stream
that multiplexes heartbeats, telemetry snapshots, job commands and job
results.

---

## Service overview

```proto
service AgentGateway {
  rpc RenewCertificate(RenewCertificateRequest) returns (RenewCertificateResponse);
  rpc Connect(stream ConnectClientMessage) returns (stream ConnectServerMessage);
}
```

| RPC              | Kind                 | Purpose                                                                                  |
|------------------|----------------------|------------------------------------------------------------------------------------------|
| `RenewCertificate` | unary              | Rotates the agent's mTLS client cert before it expires. Called from the agent side with the current cert still valid. |
| `Connect`          | bidirectional stream | The persistent agent-side stream. Agents send heartbeats/snapshots/results; the server pushes jobs and client-data requests. |

### Wire-level requirements

- **Transport:** TLS with mutual authentication. The agent presents the
  certificate obtained via `POST /api/agent/bootstrap` (or refreshed via
  `RenewCertificate`); the control-plane presents its CA-issued server cert.
  Connections without a valid client certificate are rejected at the TLS
  layer before any RPC is dispatched.
- **Keepalive (P2-REL-01):** server side advertises
  `keepalive.ServerParameters{Time: 30s, Timeout: 10s}` plus
  `EnforcementPolicy{MinTime: 10s, PermitWithoutStream: true}`. Agents mirror
  `WithKeepaliveParams{Time: 30s, Timeout: 10s}`. Idle streams are detected
  within 30-40 s of network failure.
- **Message caps:** `grpc.MaxRecvMsgSize(16 * 1024 * 1024)` and matching
  `MaxSendMsgSize` on both ends. Large discovery snapshots are expected to
  occasionally exceed 1 MB; 16 MiB leaves headroom without unbounded growth.
- **Endpoints:** gRPC gateway listens on `:8443` by default
  (`-grpc-addr`); the HTTP API listens on `:8080`. These are always separate
  ports.

---

## `Connect` stream shape

```proto
message ConnectClientMessage {
  oneof body {
    Heartbeat heartbeat = 1;
    Snapshot  snapshot  = 2;
    JobResult job_result = 3;
    JobAcknowledgement job_acknowledgement = 4;
    ClientDataResponse client_data_response = 5;
  }
}

message ConnectServerMessage {
  oneof body {
    JobCommand          job                  = 1;
    ClientDataRequest   client_data_request  = 2;
  }
}
```

Typical conversation for a steady-state agent:

1. Agent opens `Connect`, sends `Heartbeat` with `agent_id`, `node_name`,
   `fleet_group_id`, and `version`.
2. Every snapshot interval the agent pushes a `Snapshot` containing
   `InstanceSnapshot[]`, per-client `ClientUsageSnapshot[]`,
   `ClientIPSnapshot[]`, and optionally `RuntimeSnapshot`,
   `RuntimeDiagnosticsSnapshot`, `RuntimeSecurityInventorySnapshot`.
3. Server pushes `JobCommand` when an operator enqueues work; the agent
   replies out-of-band with `JobAcknowledgement` (received) and later
   `JobResult` (completed/failed).
4. Server may push `ClientDataRequest` to probe the agent's local view of a
   client secret; agent answers with `ClientDataResponse`.

---

## Key messages

### `Heartbeat`

| Field              | Type     | Notes                                                      |
|--------------------|----------|------------------------------------------------------------|
| `agent_id`         | string   | UUID v7 (see ADR 001).                                     |
| `node_name`        | string   | Operator-supplied label.                                   |
| `fleet_group_id`   | string   | Empty string when ungrouped.                               |
| `version`          | string   | Agent binary version.                                      |
| `read_only`        | bool     | Agent is in read-only mode (no Telemt mutations).          |
| `observed_at_unix` | int64    | Agent-side wall-clock timestamp.                           |

Field tag `3` is `reserved` (removed during early development — do not reuse).

### `InstanceSnapshot`

One per Telemt instance the agent manages. Fields: `id`, `name`, `version`,
`config_fingerprint`, `connected_users`, `read_only`.

### `ClientUsageSnapshot`

Per-client traffic and connection sample. Fields:

| Field                | Type   | Notes                                                                                                  |
|----------------------|--------|--------------------------------------------------------------------------------------------------------|
| `client_id`          | string |                                                                                                        |
| `traffic_delta_bytes`| uint64 | Bytes transferred since the previous snapshot.                                                         |
| `unique_ips_used`    | int32  | Distinct IPs observed in the window.                                                                   |
| `active_tcp_conns`   | int32  | Live gauge — replaces prior value on every snapshot, regardless of `seq`.                              |
| `active_unique_ips`  | int32  | Live gauge — same semantics as above.                                                                  |
| `client_name`        | string | Name as seen on the agent (used for discovery match).                                                  |
| `seq`                | uint64 | **P2-LOG-06** — monotonic per-agent sequence number. The control-plane skips `traffic_delta_bytes` when `seq <= lastSeen` (replay after reconnect) and treats `seq == 1` after a non-zero `lastSeen` as an agent restart baseline. `seq == 0` falls back to the legacy unconditional add path so pre-P2-LOG-06 agents keep working. |

The agent persists the latest emitted `seq` in `data/agent-state.json` via
`state.SaveUsageSeq` (read-modify-write so the mTLS bundle is never
clobbered on the hot snapshot path).

### `ClientIPSnapshot`

Per-client directory of IPs with link/limit metadata. Fields include
`client_id`, `active_ip_list[]`, `connection_link`, `max_tcp_conns`,
`expiration`, `data_quota_bytes`, `max_unique_ips`, `secret`, `user_ad_tag`,
`enabled`. Used both for telemetry and as the basis for discovered-client
records.

### `RuntimeSnapshot`

Large message carrying Telemt runtime status: `accepting_new_connections`,
`me_runtime_ready`, startup progress, me/me2dc routing flags, top connection
entries, etc. Emitted alongside the per-client snapshots.

### `JobCommand` / `JobAcknowledgement` / `JobResult`

| Message             | Direction     | Purpose                                                        |
|---------------------|---------------|----------------------------------------------------------------|
| `JobCommand`        | server → agent | Instructs the agent to perform work (reload, update, etc.).    |
| `JobAcknowledgement`| agent → server | Agent confirms it has received and accepted the command.       |
| `JobResult`         | agent → server | Final status: success + output, or failure + error message.    |

The control-plane correlates these via `job_id`. Unacknowledged jobs are
retried until their TTL expires; results are persisted to the audit trail.

### `ClientDataRequest` / `ClientDataResponse`

Server-initiated probe for local client state on a specific agent. Used
when reconciling discovered clients and resolving secret conflicts.

### `RenewCertificateRequest` / `RenewCertificateResponse`

Unary call made from inside the mTLS-authenticated transport. The response
carries a freshly-signed `certificate_pem`, `private_key_pem`, the current
`ca_pem`, and the new `expires_at_unix`. The agent persists the rotated
bundle atomically before closing the previous connection.

---

## Code locations

| Concern                              | File                                                   |
|--------------------------------------|--------------------------------------------------------|
| Proto source                         | `proto/agent_gateway.proto`                            |
| Generated Go stubs                   | `internal/gatewayrpc/`                                 |
| Control-plane gRPC server assembly   | `cmd/control-plane/main.go` (`newControlPlaneGRPCServer`) |
| Agent dial options                   | `cmd/agent/main.go`                                    |
| Stream handling on the server        | `internal/controlplane/server/agent_flow.go`, `grpc_gateway.go` |
| Snapshot dedup logic (P2-LOG-06)     | `internal/controlplane/server/grpc_gateway.go`         |

---

## Regeneration

After editing the `.proto`:

```sh
buf generate            # or `make proto` — uses buf.gen.yaml
go build ./...          # verify stubs compile
go test ./internal/gatewayrpc/... ./internal/controlplane/server/...
```

The generated Go code under `internal/gatewayrpc/` is committed — do not edit
those files by hand.
