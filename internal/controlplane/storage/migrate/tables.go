package migrate

// migratedTables and skippedTables are the single source of truth for
// which schema tables the offline sqlite→postgres copy carries (finding
// L-5). Every table in the live schema must appear in exactly one of the
// two maps — TestEverySchemaTableIsClassified enforces this so a new
// table cannot be added without a deliberate decision.
//
// migratedTables maps a table name to a short note on HOW it is copied
// (typed Store methods, raw row-copy, or as a singleton). skippedTables
// maps a table name to WHY it is intentionally not copied (transient /
// recoverable / derived state that a fresh control-plane rebuilds).

// migratedTables: tables whose durable state the migration copies.
//
// such as "cp_secrets" are schema identifiers, not hardcoded credentials.
//
//nolint:gosec // G101: map of DB table names → human descriptions; values
var migratedTables = map[string]string{
	// --- typed Store-method copies (Tier 0, pre-existing) ---
	"users":                             "typed: ListUsers → PutUser",
	"user_appearance":                   "typed: ListUserAppearances → PutUserAppearance",
	"fleet_groups":                      "typed: ListFleetGroups → PutFleetGroup",
	"agents":                            "typed: ListAgents → PutAgent",
	"telemt_instances":                  "typed: ListInstances → PutInstance",
	"jobs":                              "typed: ListJobs → PutJob",
	"job_targets":                       "typed: ListJobTargets → PutJobTarget",
	"audit_events":                      "typed: ListAuditEvents → AppendAuditEvent",
	"metric_snapshots":                  "typed: ListMetricSnapshots → AppendMetricSnapshot",
	"enrollment_tokens":                 "typed: ListEnrollmentTokens → PutEnrollmentToken",
	"agent_certificate_recovery_grants": "typed: ListAgentCertificateRecoveryGrants → PutAgentCertificateRecoveryGrant",
	"clients":                           "typed: ListClients → PutClient",
	"client_assignments":                "typed: ListClientAssignments → PutClientAssignment",
	"client_deployments":                "typed: ListClientDeployments → PutClientDeployment",
	"panel_settings":                    "typed singleton: GetPanelSettings → PutPanelSettings",
	"certificate_authority":             "typed singleton: GetCertificateAuthority → PutCertificateAuthority",
	"discovered_clients":                "typed: ListDiscoveredClients → PutDiscoveredClient",

	// --- typed Store-method copies (Tier 1 + Tier 2, finding L-5) ---
	"agent_revocations":        "typed: ListAgentRevocations → PutAgentRevocation",
	"agent_fallback_state":     "typed: ListAgentFallbackState → PutAgentFallbackState",
	"integration_providers":    "typed: ListIntegrationProviders → CreateIntegrationProvider",
	"fleet_group_integrations": "typed (per fleet group): ListFleetGroupIntegrations → CreateFleetGroupIntegration",
	"user_fleet_group_scopes":  "typed (per user): ListUserFleetGroupScopes → SetUserFleetGroupScopes",
	"client_usage":             "typed: ListClientUsage → UpsertClientUsage",
	"client_ip_history":        "typed (per client): ListClientIPHistory → UpsertClientIPHistory",
	"sessions":                 "typed: ListSessions → PutSession",
	"update_config":            "typed singletons: GetUpdate{Settings,State},GetGeoIP{Settings,State} → Put*",
	"cp_secrets":               "typed (raw bytes): ListCPSecrets → PutCPSecret",

	// --- raw row-copy (ciphertext / out-of-MigrationStore registries) ---
	"webhook_endpoints": "raw row-copy: ciphertext column copied verbatim, no encrypt/decrypt",
	"webhook_outbox":    "raw row-copy: payload copied verbatim",
	"runtime_settings":  "raw row-copy: settings registry kv table copied verbatim",

	// --- Telemt config targets (operator-desired config per scope) ---
	"agent_config_targets": "typed: ListAgentConfigTargets → UpsertAgentConfigTarget",
}

// skippedTables: tables intentionally NOT copied, each with its reason.
var skippedTables = map[string]string{
	"goose_db_version": "schema bookkeeping — recreated by the target's own migration run before copy",

	// transient auth / replay-prevention state — safe to drop, rebuilds
	// naturally on next login / TOTP use / enrollment.
	"consumed_totp":       "transient: TOTP replay window (~90s), expires almost immediately",
	"login_lockouts":      "transient: per-account lockout counters, rebuild on next failed login",
	"enrollment_attempts": "transient: short-lived enrollment diagnostics, not durable fleet state",
	"enrollment_events":   "transient: child of enrollment_attempts (FK ON DELETE CASCADE); copying without the skipped parent would violate FK",

	// derived / current-snapshot telemetry — re-reported by agents on the
	// next telemetry tick after cut-over.
	"ts_server_load":                    "recoverable: raw load timeseries, re-collected by agents post cut-over",
	"ts_server_load_hourly":             "derived: hourly rollup of ts_server_load, recomputed by the rollup worker",
	"ts_dc_health":                      "recoverable: raw DC-health timeseries, re-collected post cut-over",
	"telemt_runtime_current":            "derived snapshot: overwritten by the next agent telemetry report",
	"telemt_runtime_dcs_current":        "derived snapshot: overwritten by the next agent telemetry report",
	"telemt_runtime_upstreams_current":  "derived snapshot: overwritten by the next agent telemetry report",
	"telemt_diagnostics_current":        "derived snapshot: overwritten by the next agent telemetry report",
	"telemt_security_inventory_current": "derived snapshot: overwritten by the next agent telemetry report",
	"telemt_runtime_events":             "recoverable: runtime event ring buffer, re-populated post cut-over",

	// group config-apply rollout batches (Phase A storage layer) — no
	// bulk list-all method exists yet (only ListRunningConfigApplyBatches,
	// scoped to active rollouts, plus a per-id Get), so the offline
	// migrate-schema copy cannot enumerate historical batches. Revisit if
	// a later phase needs full batch-history parity across backends.
	"config_apply_batches":       "not yet covered by offline migrate-schema copy — see comment above",
	"config_apply_batch_targets": "child of config_apply_batches (FK ON DELETE CASCADE); skipped alongside its parent",
}
