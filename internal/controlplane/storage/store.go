package storage

import (
	"context"
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
	ListAuditEvents(ctx context.Context) ([]AuditEventRecord, error)
}

// MetricStore persists aggregated control-plane metric snapshots.
type MetricStore interface {
	AppendMetricSnapshot(ctx context.Context, snapshot MetricSnapshotRecord) error
	ListMetricSnapshots(ctx context.Context) ([]MetricSnapshotRecord, error)
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
	PruneClientIPHistory(ctx context.Context, olderThan time.Time) (int64, error)
	RollupServerLoadHourly(ctx context.Context, bucketHour time.Time) error
	ListServerLoadHourly(ctx context.Context, agentID string, from time.Time, to time.Time) ([]ServerLoadHourlyRecord, error)
	PruneServerLoadHourly(ctx context.Context, olderThan time.Time) (int64, error)
}

// Store aggregates the persistence capabilities required by the control-plane.
type Store interface {
	UserStore
	UserAppearanceStore
	FleetStore
	JobStore
	AuditStore
	MetricStore
	TelemetryStore
	EnrollmentStore
	AgentCertificateRecoveryGrantStore
	PanelSettingsStore
	CertificateAuthorityStore
	ClientStore
	DiscoveredClientStore
	TimeseriesStore

	// Ping verifies that the database connection is alive.
	Ping(ctx context.Context) error
	Close() error
}
