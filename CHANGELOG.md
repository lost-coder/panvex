# Changelog

All notable changes to Panvex are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Performance / Observability — Sprint S-20 (2026-05-02)

- **P-02:** added per-request DB query counter + N+1 detection middleware. Storage backends (`postgres` + `sqlite`) now wrap their `dbExecutor` with `instrumentedExecutor`, which increments a context-bound counter on every `Exec`/`Query`/`QueryRow` call. HTTP middleware `dbQueryCountMiddleware` installs a fresh counter per inbound request and emits a structured WARN with `alert=high_db_query_count`, `path`, `method`, `query_count`, `threshold` when the count exceeds **30** for a single request — that's the audit's N+1 paging signal. Calls running outside a tracked HTTP request (background batch writer, gRPC streams, startup hooks) are unaffected — `IncrementDBQuery` is a cheap atomic no-op when the context carries no counter. Closes the audit's "no SQL tracing → can't confirm N+1" gap; gives operators the dashboard signal needed to drill into specific endpoints. Added 8 unit tests covering counter semantics (zero-init, increments, request-scoping, thread safety, no-op when ctx unset) and middleware behaviour (under-threshold quiet, above-threshold WARN with audit-stable alert key, no-logger safety).

### Security / DX — Sprint S-19 (2026-05-02)

- **BP-02 (final):** completed the Zod schema sweep across all 9 API files in `web/src/shared/api/`. Every `api<T>(...)` call that returns a non-empty body now passes a Zod schema as the third argument; responses are validated at the boundary instead of cast `as T`. Closes the audit's "no-schema migration list" TODO. Added 36 new response schemas for endpoints that previously lacked one. The full migration matches the pattern Sprint S-12 established for auth.ts. Exact file-by-file coverage:
  - `users.ts`: already covered from S-12 (userSchema, userListSchema) — 0 new schemas.
  - `enrollment.ts`: already covered from S-12 (enrollmentTokenResponseSchema, enrollmentTokenListSchema) — 0 new schemas.
  - `jobs.ts`: already covered from S-12 (jobSchema, jobListSchema, auditEventListSchema) — 0 new schemas.
  - `settings.ts`: already covered (panelSettingsResponseSchema, appearanceSettingsResponseSchema, retentionSettingsSchema, versionSchema) — 0 new schemas.
  - `updates.ts`: 5 new schemas in `schemas/updates.ts` — updateSettingsSchema, updateSettingsResponseSchema, checkForUpdatesResponseSchema, updatePanelResponseSchema, updateAgentResponseSchema.
  - `servers.ts`: 2 new schemas added to `schemas/agent.ts` — instanceSchema, instanceListSchema; agentCertificateRecoverySchema promoted to export; agentSchema reused for mutation responses.
  - `telemetry.ts`: 8 new schemas in `schemas/telemetry.ts` — fleetResponseSchema, metricSnapshotSchema/List, telemetryDetailBoostSchema, telemetryDiagnosticsRefreshResponseSchema, telemetryServersResponseSchema, telemetryServerDetailResponseSchema, serverLoadHistoryResponseSchema, dcHealthHistoryResponseSchema.
  - `clients.ts`: 4 new schemas in `schemas/clients.ts` — adoptDiscoveredClientResponseSchema, bulkAdoptDiscoveredResponseSchema, clientIPHistoryResponseSchema, bulkClientResponseSchema; clientSchema reused for 5 mutation endpoints.
  - `fleet-groups.ts`: 7 new schemas in `schemas/fleet-groups.ts` — fleetGroupDeletionResultSchema, integrationKindSchema/List, integrationProviderKindSchema/List, integrationProviderSchema/List; fleetGroupIntegrationSchema reused for integration CRUD.
  - Final coverage: 70 non-void api calls, 70 with schema — 100%.

### Code Quality — Sprint S-18 (2026-05-02)

- **Q-11:** decomposed the four edge-of-threshold feature pages into per-component sibling files.
  - `EnrollmentWizard.tsx`: 538 → **33** LOC. Extracted `ConfigureStep`, `InstallStep`, `ConnectStep` into `enrollment/steps/*.tsx`.
  - `ActivityPage.tsx`: 500 → **274** LOC. Extracted `JobsTable` and `AuditList` into `activity/components/*.tsx` (plus all helper cells/grouping logic).
  - `DashboardPage.tsx`: 479 → **160** LOC. Extracted `KpiStrip`, `FleetPanel` (with FleetList + LoadCell as internals), `TimelinePanel` into `dashboard/ui/*.tsx`; helpers `deltaClass`/`deltaArrow` moved to `dashboard/format.ts`.
  - `ServersPage.tsx`: 477 → **241** LOC. Extracted `ServerCardView` and `ServerListView` into `servers/ui/*.tsx`.
  - Pure refactor — no behaviour change, no prop signature changes; vitest suite (299 tests across 55 files) all green; ESLint baseline unchanged.

### Documentation — Sprint S-17 (2026-05-02)

- **Q-08:** added an authoritative "Terminology — node vs agent vs server" section to `src/CLAUDE.md` that maps the three overlapping terms to their concrete usage layers (DB / Go / UI). Resolves the documentation drift the audit flagged: spec docs say "node", DB calls them `agents`, frontend calls them "Servers". The new section locks down: DB and Go canonicalise on `agent` / `agents` / `agent_id`; UI copy uses "Server"; the `node_name` column on `agents` is the operator-supplied display name and is the single intentional surviving "node" reference in the schema. New code follows this mapping; existing 95 backend "node" + 204 frontend "node" mentions stay where they are because they correctly refer to the conceptual domain entity.

### DX / Performance — Sprint S-16 (2026-05-02)

- **BP-04:** replaced ad-hoc `console.error` calls in React Query `onError` handlers with `notifyMutationError(feature, action, err)`. The helper dispatches a structured `panvex:mutation-error` CustomEvent on `window` so a single ToastProvider (or future Sentry-bridge) handles every mutation failure uniformly — previously each hook/page logged independently and operators got nothing actionable in production. The dev-only `console.error` is kept inside the helper (drops out of production via `process.env.NODE_ENV` constant folding). Migrated 14 call-sites across `useProfileTotp`, `ProfilePage`, and `useServerMutations`. The auth + servers feature folders now have zero raw `console.error` in `onError` handlers.
- **P-07:** added the first `React.memo` prop-stability regression test, anchored on `PulseGrid` — the canonical hot-path memo (re-rendered on every WebSocket telemetry tick of the active server-detail tab). The test asserts that consecutive renders with the same `items` reference do NOT remount the underlying section element, and that a fresh array DOES propagate. Pattern is documented in the test header so future memo'd components (`DcTiles`, `HealthRadar`, `TimelineStrip`, …) can drop in similar coverage when they show up as profiling hot spots.
- **P-05** (already done in Sprint S-3): `auth.Service.mu` is `sync.RWMutex` and read paths use `RLock`. Confirmed; no further work required.
- **P-06** (deferred as no-op micro-opt): `metricsAuditMu` protects the audit ring buffer + `auditSeq` counter together. Splitting the counter into `atomic.Uint64` would not reduce lock contention because the lock is still required for the buffer write. Without measured contention this is premature optimization — leaving the design unchanged.

### Security — Sprint S-15 (2026-05-02)

- **S-07:** added an opt-in separate-listener mode for pprof. Set `PANVEX_PPROF_ADDR` (e.g. `127.0.0.1:6060`) to route `/debug/pprof/*` away from the admin panel onto a dedicated HTTP listener bound to the configured address — typically loopback. Default (env unset) preserves the existing admin-router behaviour. Closes the audit concern that an admin-tier panel user could pull `goroutine?debug=2` stacks containing transient secrets in local variables: with the dedicated listener, pprof requires shell on the host (or a port-forward) and is unreachable through the panel session. Helm chart values gain `pprofAddr` to wire the env var through the Deployment template. Added 3 unit tests covering the toggle flag, fail-closed behaviour when unconfigured, and an end-to-end HTTP round-trip against the standalone listener serving the pprof index.

### Self-distribution — Sprint S-14 (2026-05-02)

- **Q-05:** the agent install script is now embedded into the control-plane binary and served at `GET /install-agent.sh`. The `curl <panel>/install-agent.sh | sudo bash -s -- ...` one-liner generated by `POST /api/agents/{id}/install-command` works against any reachable panel — no external CDN required, no `https://get.panvex.io` dependency. Operators with a custom domain can still override via `PANVEX_INSTALL_SCRIPT_URL`. Moved the canonical script from `deploy/install-agent.sh` to `internal/controlplane/server/install_agent.sh` to colocate it with the `//go:embed` directive; `.dockerignore` and README updated. Added 2 unit tests asserting the script is embedded with a bash shebang and that the HTTP handler serves the verbatim body with `Content-Type: text/x-shellscript`. Removed three stale `TODO(install-command)` comments.

### Deployment — Sprint S-13 (2026-05-02)

- **BP-06:** added a minimal production Helm chart at `deploy/helm/panvex/`. Covers Deployment + Service + Secret + Ingress + PodDisruptionBudget + a one-shot `bootstrap-admin` Job (Helm `post-install` hook). The chart deliberately does NOT bundle Postgres — operators bring their own (managed cloud DB, in-cluster CloudNativePG/Crunchy/Zalando, or external) and pass the DSN via `storage.dsn` (or `storage.existingSecret` for external-secrets / sealed-secrets workflows). `encryptionKey` is required and treated as load-bearing — rotating it without re-encrypting the database invalidates every PVS2 row. Includes a comprehensive `README.md` documenting required values, quick-start, bootstrap procedure, and audit-related notes.
- **S-04:** chart sets `terminationGracePeriodSeconds: 60`, exactly 15 s above `controlPlaneShutdownGraceMin = 45 s`, so the in-flight audit-event flush completes before the kubelet sends SIGKILL. Includes a `PodDisruptionBudget` with `minAvailable: 1` so node drains don't void in-flight requests when running multiple replicas.

### Security / DX — Sprint S-12 (2026-05-02)

- **BP-02 (part 1):** wired Zod runtime validation onto every response in `web/src/shared/api/auth.ts`. Added `totpSetupResponseSchema` and `totpStatusResponseSchema` to `schemas/auth.ts`; the `login`, `startTotpSetup`, `enableTotp`, and `disableTotp` API calls now parse responses through their schemas instead of casting `as T`. Auth is the highest-leverage area to migrate first — every other endpoint protects already-authenticated state, so a schema-mismatch on auth responses now fails loudly rather than corrupting the login flow. Added 4 schema tests covering shape acceptance + edge-case rejection (empty secret, non-boolean state).
- **BP-02 (remaining scope):** ~60 more endpoints across `clients.ts` (12), `fleet-groups.ts` (15), `telemetry.ts` (8), `settings.ts` (8), `servers.ts` (6), `users.ts` (4), `enrollment.ts` (3), `updates.ts` (5), `jobs.ts` (1) still cast `as T`. The pattern is established and tested; full migration is mechanical but per-endpoint work — owned by each feature team during ongoing maintenance. Audit `BP-02` partially closed; remaining endpoints can be tracked individually.

### Performance — Sprint S-11 (2026-05-02)

- **P-04:** lazy-loaded the three `ServerDetail` tab components (`ConnectionsTab`, `MePoolTab`, `EventsTab`) via `React.lazy` + `Suspense`. The initial `ServerDetailContainer-*.js` chunk shrunk from ~30 KB to **12.79 KB gzip**; each tab now streams in its own ~5–10 KB chunk only when the user activates it (mobile swipe / desktop Fold open). Tightened the `size-limit` page-chunk budget from 60 KB to 20 KB to lock the win in CI. The smaller initial chunk reduces time-to-interactive on first navigation to a server's detail page, especially on flaky networks where each round-trip costs.
- **Q-11 (deferred):** the four large feature-page tsx files (`EnrollmentWizard.tsx` 538 LOC, `ActivityPage.tsx` 500, `DashboardPage.tsx` 479, `ServersPage.tsx` 477) remain at-size — each is at the edge of the recommended LOC threshold but already lazy-loaded at the route level via TanStack Router's `lazyRouteComponent`. Splitting their internal step/section components is feature-extraction work better done in a dedicated sprint with corresponding test coverage; deferring intentionally rather than risking a bug at the end of an audit-driven cleanup.

### Documentation — Sprint S-10 (2026-05-02)

- **Q-06:** removed two stale TODO comments. `bootstrap/enroll.go` no longer claims `EnrollDriver` is unwired — the production path constructs an `enrollFn` closure in `cmd/control-plane/serve.go` and registers it via `agenttransport.Manager.SetEnrollCallbacks`. `agenttransport/manager.go` no longer claims outbound TLS is unwired — production passes `api.GRPCTLSConfig()` (the panel's mTLS config), and Sprint S-1 cert-pinning layers SPKI verification on top.
- **Q-07:** rewrote the `state_restore.restoreFallbackState` doc-block to acknowledge that the inline `slog.Error` with stable `alert=streamAlerts["fallback_state"]` attribute is intentional and adequate. The previous TODO suggested unifying with the batch writer's emission pipeline, but the batch writer's retry/queue machinery is for high-frequency background streams; a one-shot startup hook does not need it. Operators page on the alert key, not the call path.
- **S-11:** added a `# DEV-ONLY compose profile` header to `deploy/docker-compose.sqlite.yml`, mirroring the existing comment in `deploy/docker-compose.postgres.yml`. Both dev compose files now explicitly warn against production use and point to `docker-compose.prod.yml`.

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

## [Unreleased] — Sprint S-4 (2026-05-02)

### Code Quality — Sprint S-4 (2026-05-XX)

- **Q-01a:** decomposed `cmd/control-plane/main.go` (was 1242 LOC) into nine files within `package main`. `main.go` is now 200 LOC and contains only `main()`, the subcommand router `run()`, build-vars, shared helpers (`openStore`, `resolveEncryptionKey`, `parseCIDRList`, `parseLogLevel`, `openLogSink`, `installScriptURL`), and the storage-flag constants. Subcommands moved to dedicated files: `serve.go` (runServe, parseServeConfig, resolvePanelRuntime, newAPIServer), `server_lifecycle.go` (HTTP/gRPC server constructors, start/shutdown helpers, waitForServeShutdown, plus all timeout/keepalive constants), `otel.go` (initOtelTracing / shutdownOtel), `bootstrap_admin.go`, `reset_user_totp.go`, `migrate_storage.go`, `migrate_schema.go`, `self_update.go` (with archive download/verify helpers). Pure refactor — no behavioural change; all `cmd/control-plane` tests continue to pass under `-race`.

## [Unreleased] — Sprint S-5 (2026-05-02)

### Code Quality — Sprint S-5 (2026-05-XX)

- **Q-01b:** decomposed `cmd/agent/main.go` (was 1509 LOC) into eight files within `package main`. `main.go` is now ~95 LOC and contains only `main()`, the `run()` dispatcher, build-vars (`AgentVersion`, `CommitSHA`, `BuildTime`), `agentDeregisteredExitCode` sentinel, the timeout / queue-capacity constants shared across files (`runtimeCertificateRenewWindow`, `runtimeCertificateRenewRetry`, `certificateRefreshTimeout`, `gatewayStreamConnectTimeout`, `jobExecutionTimeout`, `runtimeOperationTimeout`, `jobQueueCapacity`, `runtimeInitializationFastInterval`), and the shared helpers (`clientDataConcurrencyDefault`, `hostName`, `parseLogLevel`). Major subsystems moved to dedicated files: `runtime.go` (runRuntime, parseRuntimeFlags, reconnect loop), `schedule.go` (connectionSchedule, polling-group config, ticker helpers), `polling.go` (heartbeat / usage / IP / runtime-poll workers), `jobs.go` (job inflight tracker + workers), `outbound.go` (outbound message pump, stream setup timeout, client-data request handler), `connection.go` (runConnection, selectTransport, transportReloadState), `credentials.go` (cert/credential refresh + CSR/cert PEM encoding). Pure refactor — no behavioural change; all `cmd/agent` tests continue to pass under `-race`. `bootstrap.go` and `bootstrap_reverse.go` were already extracted in earlier work and remain untouched.

## [Unreleased] — Sprint S-6 (2026-05-02)

### Code Quality — Sprint S-6 (2026-05-02)

- **Q-03:** decomposed `internal/agent/telemt/client.go` (was 1465 LOC) into nine files within `package telemt`. `client.go` is now ~180 LOC and holds only the `Config` / `Client` structs, constructor `NewClient`, the loopback validator, `getJSONPayload` wrapper, path constants and error sentinels. Logic moved to: `types.go` (public `Runtime*` data types and methods, `ManagedClient`, `ClientUsage`), `internal_types.go` (private wire shapes used during JSON decoding), `runtime_state.go` (FetchRuntimeState + fast fetchers + assembly), `slow_state.go` (heavier-cadence fetchers — upstream status, recent events, security inventory, me-writers summary, slow diagnostics), `client_usage.go` (client-usage fetching from JSON + Prometheus metrics, `ClientUsageMetricsSnapshot`), `convert.go` (small wire→public converters), `http.go` (HTTP/JSON helpers — `getJSON`, `newRequest`, decode helpers, scope parsing), `client_crud.go` (managed-client CRUD: `CreateClient`, `UpdateClient`, `DeleteClient`, `FetchActiveIPs`, `ExecuteRuntimeReload`). Pure refactor — no behavioural change; public `Client` API unchanged; all `internal/agent/telemt` tests pass under `-race`.

## [Unreleased] — Sprint S-7 (2026-05-02)

### Code Quality — Sprint S-7 (2026-05-XX)

- **Q-04 (partial):** decomposed `internal/controlplane/server/grpc_gateway.go` (was 1128 LOC) into seven files within `package server`. `grpc_gateway.go` is now 215 LOC and holds only the top-level gRPC entry points (`Connect`, `RunAgentSession`, `RenewCertificate`), the session orchestrator `runAgentSession`, the connection authorizer `authorizeAgentConnect`, auth helpers (`authenticatedAgentID`, `authenticatedAgentIdentity`), and the const block. Logic moved to: `gateway_stream.go` (agentStreamChannels + start*Loop helpers + recoverAgentStreamGoroutine + awaitAgentStreamShutdown), `gateway_messages.go` (inbound message processing — priority/regular dispatch, in-stream cert renewal), `gateway_effects.go` (priority result/audit queues + drain helpers + regular snapshot queue), `gateway_snapshots.go` (handleSnapshotMessage + protobuf→storage converters for instances / client-usage / client-ip), `gateway_jobs.go` (job dispatch + delivery/ack/result tracking), `gateway_revocation.go` (per-stream revocation watcher). Pure refactor — no behavioural change; gRPC service contract unchanged; all `internal/controlplane/server` tests pass under `-race`. The remaining server god-files (`clients_flow.go`, `agent_flow.go`, `http_clients.go`) will be tackled in a follow-up sprint.

## [Unreleased] — Sprint S-8 (2026-05-02)

### Code Quality — Sprint S-8 (2026-05-XX)

- **Q-04 (final):** completed decomposition of remaining server god-files. `clients_flow.go` (was 988 LOC) split into `clients_flow.go` (CRUD methods + `applyClientMutationFields`), `clients_state.go` (snapshot/restore/listing), `clients_jobs.go` (job dispatch). `agent_flow.go` (was 925 LOC) split into `agent_flow.go` (top-level enrollAgent + applyAgentSnapshot), `agent_snapshot.go` (snapshot apply pipeline + commit-locked helpers + fallback transitions), `agent_telemetry.go` (runtime conversions + batch-write enqueue). `http_clients.go` (was 783 LOC) split into `http_clients.go` (HTTP handlers + `clientMutationRequest`) and `http_clients_helpers.go` (scope/auth helpers + bulk operations + helpers like `buildClientListRow` / `handleClientMutationError` / `buildClientDetailResponse`). Pure refactor — no behavioural change; HTTP and gRPC contracts unchanged; all `internal/controlplane/server` tests pass under `-race`.
- **BP-01 (continuation):** removed `*WithContext` paired methods on the server flows that survived Sprint S-3. Methods now accept `ctx context.Context` as the first argument: `createClient`, `updateClient`, `rotateClientSecret`, `deleteClient`, `enrollAgent`, `applyAgentSnapshot`. The non-ctx wrappers are gone. HTTP handlers and tests updated.

## [Unreleased] — Sprint S-9 (2026-05-02)

### Security / DX — Sprint S-9 (2026-05-XX)

- **S-08:** `Content-Security-Policy` `connect-src` directive now scopes WebSocket connections to the request host (`wss://<host>`) instead of the unbounded `wss:` source. Browsers will reject WebSocket attempts to arbitrary HTTPS hosts even if a script tries them. nginx static-shell CSP updated to mirror.
- **S-09:** HSTS `preload` directive is now an opt-in via `PANVEX_HSTS_PRELOAD=1` (or `true`/`yes`/`on`). Default HSTS remains `max-age=31536000; includeSubDomains`. When opted in, `max-age` extends to 2 years (63 072 000 s) per the HSTS preload policy.
- **S-10:** `bootstrap-admin` subcommand now accepts `-password-file <path>` (or `PANVEX_BOOTSTRAP_PASSWORD_FILE` env) to read the admin password from a file. Resolution order: file flag → file env → password flag → password env. Recommended path for production setups using systemd `LoadCredential=` or Docker secrets — file-based reading avoids leaking the password into `/proc/<pid>/environ` or `docker inspect` output.
