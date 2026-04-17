package storage

import (
	"context"
	"encoding/json"
	"time"
)

// UserStore persists local control-plane user records.
type UserStore interface {
	PutUser(ctx context.Context, user UserRecord) error
	DeleteUser(ctx context.Context, userID string) error
	GetUserByID(ctx context.Context, userID string) (UserRecord, error)
	GetUserByUsername(ctx context.Context, username string) (UserRecord, error)
	ListUsers(ctx context.Context) ([]UserRecord, error)
}

// UserAppearanceStore persists per-user appearance preferences.
type UserAppearanceStore interface {
	PutUserAppearance(ctx context.Context, appearance UserAppearanceRecord) error
	GetUserAppearance(ctx context.Context, userID string) (UserAppearanceRecord, error)
	ListUserAppearances(ctx context.Context) ([]UserAppearanceRecord, error)
}

// FleetStore persists fleet topology and discovered Telemt runtime state.
type FleetStore interface {
	PutFleetGroup(ctx context.Context, group FleetGroupRecord) error
	ListFleetGroups(ctx context.Context) ([]FleetGroupRecord, error)
	PutAgent(ctx context.Context, agent AgentRecord) error
	ListAgents(ctx context.Context) ([]AgentRecord, error)
	DeleteAgent(ctx context.Context, agentID string) error
	UpdateAgentNodeName(ctx context.Context, agentID string, nodeName string) error
	PutInstance(ctx context.Context, instance InstanceRecord) error
	ListInstances(ctx context.Context) ([]InstanceRecord, error)
	DeleteInstancesByAgent(ctx context.Context, agentID string) error
}

// JobStore persists orchestration jobs and per-target result state.
type JobStore interface {
	PutJob(ctx context.Context, job JobRecord) error
	GetJobByIdempotencyKey(ctx context.Context, idempotencyKey string) (JobRecord, error)
	ListJobs(ctx context.Context) ([]JobRecord, error)
	PutJobTarget(ctx context.Context, target JobTargetRecord) error
	ListJobTargets(ctx context.Context, jobID string) ([]JobTargetRecord, error)
}

// AuditStore persists immutable operator and security events.
type AuditStore interface {
	AppendAuditEvent(ctx context.Context, event AuditEventRecord) error
	// ListAuditEvents returns the most recent audit events in ascending
	// chronological order. limit caps the number of rows returned; values
	// <= 0 fall back to a hard maximum of 1024.
	ListAuditEvents(ctx context.Context, limit int) ([]AuditEventRecord, error)
	// PruneAuditEvents deletes audit_events rows with created_at strictly
	// before the cutoff and returns the number of deleted rows. Used by the
	// retention worker (P2-REL-04 / finding M-R2) to keep audit_events from
	// growing unbounded.
	PruneAuditEvents(ctx context.Context, before time.Time) (int64, error)
}

// MetricStore persists aggregated control-plane metric snapshots.
type MetricStore interface {
	AppendMetricSnapshot(ctx context.Context, snapshot MetricSnapshotRecord) error
	ListMetricSnapshots(ctx context.Context) ([]MetricSnapshotRecord, error)
	// PruneMetricSnapshots deletes metric_snapshots rows with captured_at
	// strictly before the cutoff and returns the number of deleted rows.
	// Used by the retention worker (P2-REL-05).
	PruneMetricSnapshots(ctx context.Context, before time.Time) (int64, error)
}

// TelemetryStore persists current Telemt telemetry projections and recent runtime events.
type TelemetryStore interface {
	PutTelemetryRuntimeCurrent(ctx context.Context, record TelemetryRuntimeCurrentRecord) error
	GetTelemetryRuntimeCurrent(ctx context.Context, agentID string) (TelemetryRuntimeCurrentRecord, error)
	ListTelemetryRuntimeCurrent(ctx context.Context) ([]TelemetryRuntimeCurrentRecord, error)
	ReplaceTelemetryRuntimeDCs(ctx context.Context, agentID string, records []TelemetryRuntimeDCRecord) error
	ListTelemetryRuntimeDCs(ctx context.Context, agentID string) ([]TelemetryRuntimeDCRecord, error)
	ReplaceTelemetryRuntimeUpstreams(ctx context.Context, agentID string, records []TelemetryRuntimeUpstreamRecord) error
	ListTelemetryRuntimeUpstreams(ctx context.Context, agentID string) ([]TelemetryRuntimeUpstreamRecord, error)
	AppendTelemetryRuntimeEvents(ctx context.Context, agentID string, records []TelemetryRuntimeEventRecord) error
	ListTelemetryRuntimeEvents(ctx context.Context, agentID string, limit int) ([]TelemetryRuntimeEventRecord, error)
	PruneTelemetryRuntimeEvents(ctx context.Context, olderThan time.Time) (int64, error)
	PutTelemetryDiagnosticsCurrent(ctx context.Context, record TelemetryDiagnosticsCurrentRecord) error
	GetTelemetryDiagnosticsCurrent(ctx context.Context, agentID string) (TelemetryDiagnosticsCurrentRecord, error)
	PutTelemetrySecurityInventoryCurrent(ctx context.Context, record TelemetrySecurityInventoryCurrentRecord) error
	GetTelemetrySecurityInventoryCurrent(ctx context.Context, agentID string) (TelemetrySecurityInventoryCurrentRecord, error)
	PutTelemetryDetailBoost(ctx context.Context, record TelemetryDetailBoostRecord) error
	ListTelemetryDetailBoosts(ctx context.Context) ([]TelemetryDetailBoostRecord, error)
	DeleteTelemetryDetailBoost(ctx context.Context, agentID string) error
}

// EnrollmentStore persists one-time agent enrollment tokens.
type EnrollmentStore interface {
	PutEnrollmentToken(ctx context.Context, token EnrollmentTokenRecord) error
	ListEnrollmentTokens(ctx context.Context) ([]EnrollmentTokenRecord, error)
	GetEnrollmentToken(ctx context.Context, value string) (EnrollmentTokenRecord, error)
	ConsumeEnrollmentToken(ctx context.Context, value string, consumedAt time.Time) (EnrollmentTokenRecord, error)
	RevokeEnrollmentToken(ctx context.Context, value string, revokedAt time.Time) (EnrollmentTokenRecord, error)
}

// AgentCertificateRecoveryGrantStore persists administrator-approved certificate recovery windows.
type AgentCertificateRecoveryGrantStore interface {
	PutAgentCertificateRecoveryGrant(ctx context.Context, grant AgentCertificateRecoveryGrantRecord) error
	ListAgentCertificateRecoveryGrants(ctx context.Context) ([]AgentCertificateRecoveryGrantRecord, error)
	GetAgentCertificateRecoveryGrant(ctx context.Context, agentID string) (AgentCertificateRecoveryGrantRecord, error)
	UseAgentCertificateRecoveryGrant(ctx context.Context, agentID string, usedAt time.Time) (AgentCertificateRecoveryGrantRecord, error)
	RevokeAgentCertificateRecoveryGrant(ctx context.Context, agentID string, revokedAt time.Time) (AgentCertificateRecoveryGrantRecord, error)
}

// PanelSettingsStore persists operator-managed panel network and TLS settings.
type PanelSettingsStore interface {
	PutPanelSettings(ctx context.Context, settings PanelSettingsRecord) error
	GetPanelSettings(ctx context.Context) (PanelSettingsRecord, error)
}

// RetentionSettingsStore persists operator-managed retention windows for
// timeseries data, runtime events, and client IP history. Returns
// ErrNotFound when no row has been written yet so the caller can fall
// back to its own defaults.
type RetentionSettingsStore interface {
	GetRetentionSettings(ctx context.Context) (RetentionSettings, error)
	PutRetentionSettings(ctx context.Context, settings RetentionSettings) error
}

// RetentionSettings is the storage-layer alias used across the Store
// interface. Callers in the control-plane server wrap it with their
// own typed RetentionSettings struct; at the storage boundary this
// alias keeps the interface decoupled from server internals while
// reusing the same field layout (see RetentionSettingsRecord).
type RetentionSettings = RetentionSettingsRecord

// UpdateConfigStore persists update settings and cached version state as opaque JSON blobs.
type UpdateConfigStore interface {
	PutUpdateSettings(ctx context.Context, settings json.RawMessage) error
	GetUpdateSettings(ctx context.Context) (json.RawMessage, error)
	PutUpdateState(ctx context.Context, state json.RawMessage) error
	GetUpdateState(ctx context.Context) (json.RawMessage, error)
}

// CertificateAuthorityStore persists the control-plane root CA required for agent mTLS continuity.
type CertificateAuthorityStore interface {
	PutCertificateAuthority(ctx context.Context, authority CertificateAuthorityRecord) error
	GetCertificateAuthority(ctx context.Context) (CertificateAuthorityRecord, error)
}

// ClientStore persists centrally managed Telemt clients, rollout assignments, and per-node deployment state.
type ClientStore interface {
	PutClient(ctx context.Context, client ClientRecord) error
	GetClientByID(ctx context.Context, clientID string) (ClientRecord, error)
	ListClients(ctx context.Context) ([]ClientRecord, error)
	PutClientAssignment(ctx context.Context, assignment ClientAssignmentRecord) error
	DeleteClientAssignments(ctx context.Context, clientID string) error
	ListClientAssignments(ctx context.Context, clientID string) ([]ClientAssignmentRecord, error)
	PutClientDeployment(ctx context.Context, deployment ClientDeploymentRecord) error
	ListClientDeployments(ctx context.Context, clientID string) ([]ClientDeploymentRecord, error)
}

// DiscoveredClientStore persists Telemt users found on agents that are not managed by the panel.
type DiscoveredClientStore interface {
	PutDiscoveredClient(ctx context.Context, record DiscoveredClientRecord) error
	ListDiscoveredClients(ctx context.Context) ([]DiscoveredClientRecord, error)
	ListDiscoveredClientsByAgent(ctx context.Context, agentID string) ([]DiscoveredClientRecord, error)
	GetDiscoveredClient(ctx context.Context, id string) (DiscoveredClientRecord, error)
	// GetDiscoveredClientByAgentAndName looks up a discovered_clients row by
	// its natural key (agent_id, client_name). Returns ErrNotFound when no
	// row exists. Used by the reconcile path to dedupe repeated FULL_SNAPSHOT
	// reports from an agent so the pending-review list does not grow unbounded
	// (see P2-LOG-02, finding L-10 / M-C4).
	GetDiscoveredClientByAgentAndName(ctx context.Context, agentID string, clientName string) (DiscoveredClientRecord, error)
	UpdateDiscoveredClientStatus(ctx context.Context, id string, status string, updatedAt time.Time) error
	DeleteDiscoveredClient(ctx context.Context, id string) error
}

// TimeseriesStore persists historical metric points for server load, DC health, and client IPs.
type TimeseriesStore interface {
	AppendServerLoadPoint(ctx context.Context, record ServerLoadPointRecord) error
	ListServerLoadPoints(ctx context.Context, agentID string, from time.Time, to time.Time) ([]ServerLoadPointRecord, error)
	PruneServerLoadPoints(ctx context.Context, olderThan time.Time) (int64, error)
	AppendDCHealthPoint(ctx context.Context, record DCHealthPointRecord) error
	ListDCHealthPoints(ctx context.Context, agentID string, from time.Time, to time.Time) ([]DCHealthPointRecord, error)
	PruneDCHealthPoints(ctx context.Context, olderThan time.Time) (int64, error)
	UpsertClientIPHistory(ctx context.Context, record ClientIPHistoryRecord) error
	ListClientIPHistory(ctx context.Context, clientID string, from time.Time, to time.Time) ([]ClientIPHistoryRecord, error)
	CountUniqueClientIPs(ctx context.Context, clientID string) (int, error)
	PruneClientIPHistory(ctx context.Context, olderThan time.Time) (int64, error)
	RollupServerLoadHourly(ctx context.Context, bucketHour time.Time) error
	ListServerLoadHourly(ctx context.Context, agentID string, from time.Time, to time.Time) ([]ServerLoadHourlyRecord, error)
	PruneServerLoadHourly(ctx context.Context, olderThan time.Time) (int64, error)
}

// SessionStore persists authenticated user sessions.
type SessionStore interface {
	PutSession(ctx context.Context, session SessionRecord) error
	GetSession(ctx context.Context, sessionID string) (SessionRecord, error)
	DeleteSession(ctx context.Context, sessionID string) error
	ListSessions(ctx context.Context) ([]SessionRecord, error)
	DeleteExpiredSessions(ctx context.Context, before time.Time) error
}

// AgentRevocationStore persists deregistered-agent IDs so the revocation set
// survives control-plane restart. See AgentRevocationRecord in models.go.
type AgentRevocationStore interface {
	PutAgentRevocation(ctx context.Context, revocation AgentRevocationRecord) error
	ListAgentRevocations(ctx context.Context) ([]AgentRevocationRecord, error)
	DeleteExpiredAgentRevocations(ctx context.Context, before time.Time) (int64, error)
}

// TxFn is the callback invoked by Store.Transact. The tx argument
// implements the full Store interface so that existing methods compose
// without duplication — see P2-ARCH-01.
//
// NOTE: TxFn MUST NOT call tx.Transact recursively. Nested Transact
// calls on the same connection would deadlock (SQLite) or escalate
// isolation requirements unpredictably (PostgreSQL). Both backends
// detect the nested call and return ErrNestedTransact.
type TxFn func(tx Store) error

// Store aggregates the persistence capabilities required by the control-plane.
type Store interface {
	UserStore
	UserAppearanceStore
	SessionStore
	AgentRevocationStore
	FleetStore
	JobStore
	AuditStore
	MetricStore
	TelemetryStore
	EnrollmentStore
	AgentCertificateRecoveryGrantStore
	PanelSettingsStore
	RetentionSettingsStore
	UpdateConfigStore
	CertificateAuthorityStore
	ClientStore
	DiscoveredClientStore
	TimeseriesStore

	// Transact runs fn inside a single database transaction. The tx
	// argument is a Store implementation bound to the transaction:
	// all mutations performed through it either commit as a unit or
	// roll back together.
	//
	// Contract:
	//   - On fn returning nil, the transaction commits.
	//   - On fn returning a non-nil error, the transaction rolls back
	//     and the error is returned to the caller.
	//   - On panic inside fn, the transaction rolls back and the panic
	//     is re-raised.
	//   - Context cancellation during fn aborts the transaction.
	//   - PostgreSQL: serialization failures (SQLSTATE 40001) are
	//     retried up to 3 times automatically. Default isolation is
	//     read-committed.
	//   - SQLite: uses BEGIN IMMEDIATE so the writer lock is acquired
	//     up front. No retry loop (single-writer semantics).
	//   - TxFn MUST NOT call tx.Transact; nested calls return
	//     ErrNestedTransact immediately.
	Transact(ctx context.Context, fn TxFn) error

	// Ping verifies that the database connection is alive.
	Ping(ctx context.Context) error
	Close() error
}
