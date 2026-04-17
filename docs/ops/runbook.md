# Panvex Operational Runbook

SRE / on-call reference for operating the Panvex control-plane and agent fleet.

This document assumes familiarity with the Panvex architecture (control-plane +
PostgreSQL/SQLite store + gRPC agent mesh). For architecture background see
`docs/architecture/`. For release signing see `docs/ops/release-signing.md`.
For schema migrations see `docs/ops/migrations.md`.

---

## 1. Startup

### 1.1 Control-plane

```bash
./panvex-control-plane \
    --config=/etc/panvex/config.toml \
    --encryption-key-file=/etc/panvex/encryption.key
```

Notes:

- `--encryption-key-file` is the modern flag (P1-SEC-03). The legacy
  `--encryption-key` remains available for migration paths but is discouraged
  in production systemd units because it exposes the key on the process
  command line.
- The key file must contain the raw 32-byte AES-256 key, base64 encoded. It
  must be readable only by the control-plane user (`chmod 0400`).
- Alternatively the key may be provided via `PANVEX_ENCRYPTION_KEY`
  environment variable (base64). The flag takes precedence.

Expected boot log lines (in order, structured slog):

```
level=INFO msg="loaded config" path=/etc/panvex/config.toml
level=INFO msg="opened store" driver=postgres
level=INFO msg="applied migrations" count=N
level=INFO msg="CA initialized" subject="CN=panvex-ca"
level=INFO msg="session store ready"
level=INFO msg="batch writer started" buffers=[audit,metrics,events]
level=INFO msg="gRPC server listening" addr=:7443
level=INFO msg="HTTP server listening" addr=:8443
level=INFO msg="control-plane ready"
```

If any of the lines up to and including `CA initialized` are missing, the
process exits non-zero. `readyz` will not return 200 until the final
`control-plane ready` line is written.

### 1.2 Agent

```bash
./panvex-agent \
    --state-file=/var/lib/panvex/state.json \
    --control-plane-url=https://panvex.example.com:7443
```

Notes:

- `state.json` stores the agent's mTLS credentials, enrollment token state,
  and `cert_expires_at`. Permissions must be `0600` and owned by the agent
  user.
- On first boot, the agent performs enrollment using `--enrollment-token`
  (one-shot flag). After enrollment, the token is consumed and should be
  removed from the systemd unit.

Expected boot log lines:

```
level=INFO msg="agent starting" version=X.Y.Z
level=INFO msg="loaded state" agent_id=... cert_expires_at=...
level=INFO msg="dialing control-plane" url=...
level=INFO msg="mTLS handshake ok" cp_cert_fingerprint=...
level=INFO msg="presence stream established"
level=INFO msg="agent ready"
```

---

## 2. Configuration reference

Configuration is TOML, loaded from the path passed to `--config`.

### 2.1 Core settings

```toml
[panel_settings]
# Fully-qualified externally visible URL. Used for email links, OIDC
# redirects, signed download URLs, and the UI.
http_public_url = "https://panvex.example.com"

[retention]
# All durations are Go duration strings (e.g. "720h" = 30 days).
# All seven fields below derive from REL-03 and MUST be set explicitly;
# zero values disable pruning for that table.
audit_events       = "8760h"  # 1 year
metric_samples     = "720h"   # 30 days
event_stream       = "168h"   # 7 days
agent_heartbeats   = "24h"
session_records    = "720h"
revoked_agents     = "17520h" # 2 years
update_artifacts   = "2160h"  # 90 days

[update_channel]
# Cosign public key used to verify panel_update and agent_update artifacts.
cosign_public_key_path = "/etc/panvex/cosign.pub"
# Channel name: stable, beta, canary.
channel = "stable"
# Update feed URL (signed index).
feed_url = "https://releases.panvex.io/stable/index.json"
```

### 2.2 Environment variables

| Variable                    | Purpose                                                                 |
|-----------------------------|-------------------------------------------------------------------------|
| `PANVEX_ENCRYPTION_KEY`     | Base64-encoded AES-256 key. Alternative to `--encryption-key-file`.     |
| `PROMETHEUS_SCRAPE_TOKEN`   | Bearer token required on `GET /metrics` (P2-OBS-01).                    |
| `PANVEX_LOG_LEVEL`          | Override log level: DEBUG / INFO / WARN / ERROR. Default INFO.          |
| `PANVEX_DB_DSN`             | Overrides `[store].dsn` for environments that inject secrets at runtime.|

Secrets MUST be delivered via files or a secrets manager, not via plain
environment entries in the systemd unit (use `EnvironmentFile=` with
mode `0400`).

---

## 3. Health / readiness

| Endpoint    | Purpose                                                   | Auth               |
|-------------|-----------------------------------------------------------|--------------------|
| `/healthz`  | Liveness. Always `200 OK` while the process is alive.     | None               |
| `/readyz`   | Readiness. `200` iff CA initialized AND store reachable AND session store ready. Response body is a generic `{"status":"ok"}` / `{"status":"not ready"}` with no internal details (P1-SEC-14). | None |
| `/metrics`  | Prometheus scrape endpoint.                               | `Bearer <token>`   |

Use `/readyz` for load-balancer health checks. Use `/healthz` for process
supervision (systemd `Restart=on-failure`, Kubernetes liveness).

`/metrics` without the correct bearer token returns `401` with a generic
body — do not leak whether the endpoint exists.

---

## 4. Key metrics & dashboard

Monitor these Prometheus series at minimum:

| Metric                                              | Alert rule (suggested)                                  |
|-----------------------------------------------------|---------------------------------------------------------|
| `panvex_batch_queue_depth{buffer}`                  | `> 1000` for 5m — buffer is falling behind              |
| `panvex_batch_flush_duration_seconds{stream}`       | p99 `> 2s` for 10m — store latency                      |
| `panvex_batch_persist_errors_total{stream,type}`    | `increase(...[5m]) > 0` — any error counts              |
| `panvex_retention_pruned_rows_total{table}`         | `== 0` for 24h on large tables — prune job stalled      |
| `panvex_http_request_duration_seconds`              | p99 `> 1s` OR error rate `> 1%` for 5m                  |
| `panvex_rate_limited_total`                         | sudden spike vs 1h baseline                             |
| `panvex_agent_presence_online`                      | sustained drop vs fleet size                            |
| `panvex_ca_cert_expires_seconds`                    | `< 30d` — warn; `< 7d` — page                           |

Grafana dashboard: TODO add dashboard JSON under
`docs/ops/grafana/panvex-overview.json`. Until then, build panels manually
from the list above.

---

## 5. Common failures & playbooks

### 5.1 Database unreachable

Symptoms

- `panvex_batch_persist_errors_total{type="transient"}` climbing.
- HTTP `5xx` on audit-heavy endpoints, but `/login` still succeeds because
  audit writes are async (P2-LOG-10).
- `readyz` returns `503`.

Diagnose

1. From the control-plane host, test DB connectivity:
   ```bash
   psql "$PANVEX_DB_DSN" -c "SELECT 1"
   # or for SQLite
   sqlite3 /var/lib/panvex/panvex.db ".tables"
   ```
2. Inspect migration state:
   ```bash
   panvex-control-plane migrate-schema status --config=/etc/panvex/config.toml
   ```
3. Check DB host metrics (CPU, disk, connections).

Recover

- Once connectivity is restored, the control-plane's batch writer retries
  automatically. No manual intervention is required.
- If the DB was rebuilt from a different snapshot, verify `goose_db_version`
  matches the control-plane's expected schema. If behind, run
  `migrate-schema up`; if ahead, roll the control-plane forward.

### 5.2 Agent stuck offline

Symptoms

- Presence tracker shows agent offline; the agent process is alive on the
  host and its logs show repeated gRPC reconnect attempts.

Diagnose

1. On the agent host, inspect logs:
   ```
   journalctl -u panvex-agent -n 200
   ```
   Look for: `mTLS handshake failed`, `certificate expired`, `grpc: code=Unauthenticated`.
2. Check `cert_expires_at` in `/var/lib/panvex/state.json`.
3. On the control-plane, check whether the agent is revoked:
   ```sql
   SELECT agent_id, reason, revoked_at
     FROM revoked_agent_ids
    WHERE agent_id = '<id>';
   ```
   (table: `revoked_agent_ids`, P1-SEC-06).

Recover

- If revoked: the agent MUST NOT be simply restarted. Use the recovery grant
  flow (operator issues a new enrollment token) — see
  `docs/ops/agent-recovery.md`.
- If certificate expired: trigger the recovery grant flow; the agent
  re-enrolls and receives a fresh leaf cert.
- If cert valid and not revoked: restart the agent. If reconnect still
  fails, collect a packet capture between agent and control-plane.

### 5.3 Rate-limit lockout wave

Symptoms

- Surge of HTTP `429` responses.
- `panvex_rate_limited_total` spikes far above baseline.
- User reports of being unable to log in.

Diagnose

1. Correlate by source IP / user:
   ```promql
   topk(20, sum by (source_ip) (rate(panvex_rate_limited_total[5m])))
   ```
2. Distinguish legitimate burst (a few IPs, known batch job) from credential
   stuffing (many IPs, many usernames).

Recover

- Legitimate burst: ask the caller to back off, or temporarily widen the
  relevant limit in `[rate_limits]` (requires restart).
- Attack: leave the rate limit in place. If specific users were locked out,
  unlock via SQL:
  ```sql
  DELETE FROM rate_limit_state
   WHERE bucket = '<bucket>'
     AND subject = '<subject>';
  ```
  Replace `<bucket>` with e.g. `login_ip`, `login_user` and `<subject>` with
  the IP or username. Always scope narrowly; avoid `DELETE FROM
  rate_limit_state` with no predicate.

### 5.4 Signature verification failure (update)

Symptoms

- `panel_update` or `agent_update` job fails with error
  `signature_invalid` in audit events.
- UI shows the update as rejected.

Diagnose

1. Fetch the failing artifact and re-verify manually:
   ```bash
   cosign verify-blob \
       --key /etc/panvex/cosign.pub \
       --signature artifact.sig \
       artifact.tar.gz
   ```
2. If verify fails on the host but the artifact appears intact, suspect key
   rotation or the wrong channel's signing key.

Recover

- If the release team rotated the cosign key: distribute the new
  `cosign.pub` to all control-planes, restart, and re-trigger the update.
- If the artifact is corrupt in transit: re-download from the release feed,
  verify hash against the signed index, and retry.
- Never bypass signature verification. There is no `--skip-signature` flag
  and it is not a supported recovery path.

---

## 6. Graceful shutdown

Signal: `SIGTERM` to the control-plane PID.

Shutdown sequence:

1. HTTP server `Shutdown()` — stops accepting new requests, drains in-flight.
2. `batchWriter.Stop(10s)` — flushes in-memory buffers (audit, metrics,
   events) to the store. 10 s deadline.
3. gRPC `GracefulStop()` — agents disconnect cleanly.
4. `store.Close()` — releases DB handles.

Expected total shutdown duration: less than 15 seconds under normal load.
If it exceeds this, systemd's `TimeoutStopSec` (typically 30 s) will send
`SIGKILL`, which may drop unflushed audit entries.

Pre-shutdown checklist (for planned maintenance):

- Confirm `panvex_batch_queue_depth{buffer="audit"} == 0` (or trending to
  zero) before sending `SIGTERM`.
- Put the node out of rotation at the load balancer first so in-flight
  requests complete.
- Announce the window — agents will reconnect to other control-planes
  automatically, but operators should expect a presence blip.

---

## 7. Disaster recovery

### 7.1 Backups

- **PostgreSQL:**
  ```bash
  pg_dump --format=custom --file=panvex-$(date +%F).dump "$PANVEX_DB_DSN"
  ```
  Take at least daily, retain 30 days, store off-host.
- **SQLite:**
  ```bash
  sqlite3 /var/lib/panvex/panvex.db ".backup /backup/panvex-$(date +%F).db"
  ```
  `.backup` is an online, atomic copy — preferred over file copy.
- **Configuration & keys:** back up `/etc/panvex/config.toml`,
  `/etc/panvex/encryption.key`, and `/etc/panvex/cosign.pub` to a
  separate secrets vault. Do NOT store the encryption key in the same
  location as the database backup.

### 7.2 CA key recovery

The CA private key is stored in the DB, encrypted with the envelope format
`ENC2:` using the control-plane encryption key. Implications:

- **With the encryption key:** restore the DB, start the control-plane with
  the original `--encryption-key-file`, and the CA initializes normally.
- **Without the encryption key:** the CA key cannot be recovered. Every
  agent must re-enroll from scratch. Plan:
  1. Start a new control-plane with a new encryption key.
  2. The new CA bootstraps with a fresh root.
  3. Issue recovery grants for every agent and re-enroll them.
  4. Purge the old CA root from any trust anchors you distributed.

This is the catastrophic case. Treat the encryption key as a tier-0 secret.

### 7.3 Restore procedure

1. Stop the control-plane.
2. Restore DB from backup:
   - Postgres: `pg_restore --clean --dbname="$PANVEX_DB_DSN" panvex.dump`
   - SQLite: stop CP, replace the db file, verify `PRAGMA integrity_check`.
3. Ensure `/etc/panvex/encryption.key` matches the key used at the time
   of the backup.
4. Start the control-plane. Watch for the full boot sequence in
   section 1.1.
5. Verify `/readyz` returns 200 and a few agents successfully reconnect.

---

## 8. Logs & debugging

Log format: structured `slog` JSON to stdout. Capture with your log
aggregator (journald → Loki / Elastic / Splunk).

Standard fields present on most records:

| Field         | Meaning                                                 |
|---------------|---------------------------------------------------------|
| `time`        | RFC3339 timestamp                                       |
| `level`       | DEBUG / INFO / WARN / ERROR                             |
| `msg`         | Human-readable message                                  |
| `request_id`  | Correlates HTTP request and the audit/metric writes it triggered |
| `user_id`     | Authenticated user, if any                              |
| `agent_id`    | Agent identifier, if the record relates to an agent     |
| `alert`       | Present only on records meant to page (see below)       |

Log levels:

- `DEBUG` — verbose; off by default. Enable with `PANVEX_LOG_LEVEL=DEBUG`
  for short windows only; it logs request bodies in some paths.
- `INFO` — lifecycle events, successful operations.
- `WARN` — recoverable errors, degraded states.
- `ERROR` — failed operations; usually correlates with a metric increment.

Critical alert markers — configure your alerting to page on any log record
with `alert=` set. Current markers:

- `alert=audit_persist_failed` (P2-LOG-10) — audit write dropped after retry.
- `alert=ca_degraded` — CA operations failing.
- `alert=update_signature_invalid` — an update artifact failed signature
  verification.

These markers are stable log keys; treat them as a contract with alerting.

---

## 9. Upgrade procedure

### 9.1 Verify the release artifact

```bash
cosign verify-blob \
    --key /etc/panvex/cosign.pub \
    --signature panvex-control-plane-<version>.sig \
    panvex-control-plane-<version>.tar.gz
```

Do not proceed unless verification prints `Verified OK`.

### 9.2 Apply schema migrations

Migrations use goose. From a host with the new binary and DB access:

```bash
panvex-control-plane migrate-schema status --config=/etc/panvex/config.toml
panvex-control-plane migrate-schema up     --config=/etc/panvex/config.toml
```

Always run `status` first and confirm the pending migration list matches
the release notes.

### 9.3 Rolling restart

For a single control-plane:

1. Stop the old control-plane (`SIGTERM`, see section 6).
2. Run migrations (section 9.2).
3. Start the new control-plane.
4. Watch for the `control-plane ready` log line and `/readyz == 200`.
5. Agents reconnect automatically within their reconnect backoff window.

For multiple control-planes behind a load balancer:

1. Drain node 1 at the load balancer.
2. Upgrade node 1 (migrations run once; subsequent nodes are idempotent).
3. Return node 1 to rotation, verify agent counts recover.
4. Repeat for remaining nodes.

Agent binaries roll out via the signed `agent_update` channel — operators
do not deploy agent binaries manually.

---

## 10. Known limitations

- **Session-store failures on login** (P2-SEC-07): if the session store is
  unreachable, `/login` returns `503` rather than a fallback. The user is
  expected to retry; on-call should treat sustained 503s on `/login` as a
  session-store incident.
- **Legacy `ENC:v1` blobs** (P2-SEC-05): the background auto-migration to
  `ENC2:` requires the control-plane to be started with
  `--encryption-key`/`--encryption-key-file`. Nodes started without a key
  (dry-run tooling, schema-only utilities) cannot re-encrypt legacy blobs.
- **Grafana dashboard**: not yet shipped in-tree; see section 4 TODO.

---

## Appendix: quick references

- Release signing: `docs/ops/release-signing.md`
- Migrations: `docs/ops/migrations.md`
- Agent recovery grants: `docs/ops/agent-recovery.md` (TODO if absent)
- Remediation plan: `docs/plans/remediation-v4.md`
