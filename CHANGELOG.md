# Changelog

All notable changes to Panvex are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased] — Sprint S-1 Security Tightening (2026-05-02)

Closes 5 High/Medium-severity findings from the 2026-05-01 audit (S-01, S-02, S-03, S-05, S-06).

### Security

- **S-01:** Operator-tunable `password_min_length` on `panel_settings`. Default 10, range 8–128 enforced both in the Postgres CHECK constraint and in `auth.effectivePolicy`. Existing passwords are not invalidated; the policy applies only to creation and rotation. UI control added to the Settings page.
- **S-02:** SPKI cert-pinning for agents. `EnrollDriver.Run` captures `sha256(leaf.RawSubjectPublicKeyInfo)` after first successful enroll and persists via `Storage.UpdateAgentCertPin`. Subsequent panel→agent dials verify the served leaf cert SPKI hash via a `VerifyConnection` hook on the cloned `tls.Config`; mismatch returns `ErrCertPinMismatch` and rejects the connection. New metric `panvex_agent_cert_pin_total{result=ok|mismatch|missing}` (pre-init to zero) tracks each dial outcome. Bootstrap-token install-command TTL tightened from 24h to 5min and locked down with a regression test.
- **S-03:** Regression tests assert that login always rotates the session-ID and that the prior cookie is revoked server-side after re-login.
- **S-05:** Regression tests for `isCSRFExemptPath` lock down exact-string matching against attacker-controlled prefixes, path traversal, leading double-slash, trailing slash, and case folding.
- **S-06:** Startup WARN log emitted when the panel binds to a non-loopback address but `trusted_proxy_cidrs` is empty (silent XFF/Proto trust disable). `PANVEX_TRUSTED_PROXY_CIDRS` honoured as a flag fallback; `deploy/docker-compose.prod.yml` now ships a sensible default covering Docker bridge + K8s service CIDRs + IPv6 ULA.

### Observability

- New Prometheus counter `panvex_agent_cert_pin_total{result=ok|mismatch|missing}`.
- New WARN structured event with `alert=trusted_proxy_misconfigured` at startup.

### Migrations

- `0032_password_policy.sql` (postgres + sqlite): adds `panel_settings.password_min_length INTEGER NOT NULL DEFAULT 10`, with `CHECK (password_min_length BETWEEN 8 AND 128)` on Postgres.
- `0033_agent_cert_pin.sql` (postgres + sqlite): adds `agents.cert_spki_sha256` (BYTEA / BLOB) with `CHECK (length IN (0, 32))` on Postgres and a partial index on non-empty values. Postgres uses `CREATE INDEX CONCURRENTLY` (with `+goose NO TRANSACTION`) to avoid blocking the agents table.

### Internal API additions

- `auth.Service.SetPasswordPolicy(int32)` and `auth.DefaultPasswordMinLength = 10`.
- `storage.Store.UpdateAgentCertPin(ctx, agentID, pin) error` and `GetAgentCertPin(ctx, agentID) ([]byte, error)`. Update returns `ErrNotFound` when the agent does not exist.
- `agenttransport.CertPinReader` interface + `Manager.SetCertPinReader(reader, observer)` setter.
- `bootstrap.CertPinWriter` interface + `EnrollDriver.SetCertPinWriter(writer)` setter.
- New sentinel `agenttransport.ErrCertPinMismatch`.

## [Unreleased] — Sprint S-2 (2026-05-02)

### Performance / DX — Sprint S-2 (2026-05-XX)

- **P-01 + BP-03:** all React Query polling hooks now consult `useWsStatus()` via the new `useEventAwareInterval(slowMs, fastMs)` shared hook. While the WebSocket is `"open"`, every hook polls at a slow keep-alive cadence (3–6× the original); on disconnect/reconnect/close the original fast cadence resumes. Eliminates the dashboard-polling storm at scale and unifies what was a one-off pattern in `useActivity`.

### Internal API additions

- `web/src/shared/hooks/useEventAwareInterval(slowMs, fastMs): number` — single source of truth for WS-aware refetch cadence.

### Migrated hooks

- `useActivity` (canonical example, kept its 60s/15s cadence)
- `useUsers`, `useEnrollmentTokens`, `useDiscoveredClients`, `useFleetGroups` (servers): 30s → 90s/30s
- `useClientsList`: 15s → 60s/15s
- `useClientDetail`: 10s → 60s/10s
- `useFleetGroupsFull` (3 active intervals; `refetchInterval: false` entries preserved as intentional disables)
- `useClientIPHistory`, `useServerHistory` (×2), `useUpdates`: 60s → 300s/60s

No `staleTime` / `gcTime` settings were modified.

## [Unreleased] — Sprint S-3 (2026-05-02)

### Code Quality / DX — Sprint S-3 (2026-05-XX)

- **Q-02:** decomposed `internal/controlplane/auth/service.go` (was 1327 LOC, 30+ functions) into four focused files. `service.go` is now ~186 LOC and holds only the `Service` struct, constructors, global setters (`SetVault`, `SetNow`, `SetPasswordPolicy`), `Role` constants, `User` / `Session` / `LoginInput` types, and shared error sentinels. TOTP code (setup / enable / disable / reset / verify, replay store, vault encryption) lives in `totp.go`. Session lifecycle (`Authenticate`, `RevokeSessionsForUser`, `TouchSession`, `Logout`, persistence restore, idle/absolute TTL bookkeeping, timing-safe `dummyPasswordHash`) lives in `sessions.go`. Full user lifecycle (`BootstrapUser`, `CreateUser`, `UpdateUser`, `DeleteUser`, `GetUserByID`, snapshot/load) is consolidated in `users.go`. No behavioural change — pure refactor; the audit's stale `// see lockouts.go` comment is removed.
- **Q-09 + BP-01:** removed every `*WithContext` paired method on `auth.Service`. Public API now accepts `ctx context.Context` as the first argument: `Authenticate`, `BootstrapUser`, `CreateUser`, `UpdateUser`, `DeleteUser`, `GetUserByID`, `RevokeSessionsForUser`, `TouchSession`, `Logout`, `StartTotpSetup`, `EnableTotp`, `DisableTotp`, `ResetTotp`. Call-sites in HTTP handlers (`internal/controlplane/server/`) and the `cmd/control-plane bootstrap-admin` subcommand updated. Test fixtures pass `context.Background()`.
