package storage

import "time"

// UserRecord stores one local control-plane account.
type UserRecord struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
	TotpEnabled  bool
	TotpSecret   string
	CreatedAt    time.Time
}

// UserAppearanceRecord stores one user's persisted appearance preferences.
type UserAppearanceRecord struct {
	UserID    string
	Theme     string
	Density   string
	HelpMode  string
	UpdatedAt time.Time
}

// TelemetryRuntimeCurrentRecord stores one node's latest fast Telemt runtime summary.
type TelemetryRuntimeCurrentRecord struct {
	AgentID                   string
	ObservedAt                time.Time
	State                     string
	StateReason               string
	ReadOnly                  bool
	AcceptingNewConnections   bool
	MERuntimeReady            bool
	ME2DCFallbackEnabled      bool
	UseMiddleProxy            bool
	StartupStatus             string
	StartupStage              string
	StartupProgressPct        float64
	InitializationStatus      string
	Degraded                  bool
	InitializationStage       string
	InitializationProgressPct float64
	TransportMode             string
	CurrentConnections        int
	CurrentConnectionsME      int
	CurrentConnectionsDirect  int
	ActiveUsers               int
	UptimeSeconds             float64
	ConnectionsTotal          uint64
	ConnectionsBadTotal       uint64
	HandshakeTimeoutsTotal    uint64
	ConfiguredUsers           int
	DCCoveragePct             float64
	HealthyUpstreams          int
	TotalUpstreams            int
}

// TelemetryRuntimeDCRecord stores one node's latest DC health row.
type TelemetryRuntimeDCRecord struct {
	AgentID             string
	DC                  int
	ObservedAt          time.Time
	AvailableEndpoints  int
	AvailablePct        float64
	RequiredWriters     int
	AliveWriters        int
	CoveragePct         float64
	RTTMs               float64
	Load                float64
}

// TelemetryRuntimeUpstreamRecord stores one node's latest upstream health row.
type TelemetryRuntimeUpstreamRecord struct {
	AgentID             string
	UpstreamID          int
	ObservedAt          time.Time
	RouteKind           string
	Address             string
	Healthy             bool
	Fails               int
	EffectiveLatencyMs  float64
}

// TelemetryRuntimeEventRecord stores one recent runtime event observed for a node.
type TelemetryRuntimeEventRecord struct {
	AgentID      string
	Sequence     int64
	ObservedAt   time.Time
	Timestamp    time.Time
	EventType    string
	Context      string
	Severity     string
}

// TelemetryDiagnosticsCurrentRecord stores the latest slower diagnostics payloads for one node.
type TelemetryDiagnosticsCurrentRecord struct {
	AgentID              string
	ObservedAt           time.Time
	State                string
	StateReason          string
	SystemInfoJSON       string
	EffectiveLimitsJSON  string
	SecurityPostureJSON  string
	MinimalAllJSON       string
	MEPoolJSON           string
}

// TelemetrySecurityInventoryCurrentRecord stores the latest security inventory payload for one node.
type TelemetrySecurityInventoryCurrentRecord struct {
	AgentID      string
	ObservedAt   time.Time
	State        string
	StateReason  string
	Enabled      bool
	EntriesTotal int
	EntriesJSON  string
}

// TelemetryDetailBoostRecord stores one persisted detail boost window for a node.
type TelemetryDetailBoostRecord struct {
	AgentID    string
	ExpiresAt  time.Time
	UpdatedAt  time.Time
}

// FleetGroupRecord stores one fleet group in the global control-plane namespace.
type FleetGroupRecord struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// AgentRecord stores one enrolled host agent snapshot.
type AgentRecord struct {
	ID           string
	NodeName     string
	FleetGroupID string
	Version      string
	ReadOnly     bool
	LastSeenAt   time.Time
}

// InstanceRecord stores one Telemt runtime observed through an agent.
type InstanceRecord struct {
	ID                string
	AgentID           string
	Name              string
	Version           string
	ConfigFingerprint string
	ConnectedUsers    int
	ReadOnly          bool
	UpdatedAt         time.Time
}

// JobRecord stores one orchestration job.
type JobRecord struct {
	ID             string
	Action         string
	ActorID        string
	Status         string
	CreatedAt      time.Time
	TTL            time.Duration
	IdempotencyKey string
	PayloadJSON    string
}

// JobTargetRecord stores delivery and result state for one job target.
type JobTargetRecord struct {
	JobID      string
	AgentID    string
	Status     string
	ResultText string
	ResultJSON string
	UpdatedAt  time.Time
}

// AuditEventRecord stores one immutable control-plane audit event.
type AuditEventRecord struct {
	ID        string
	ActorID   string
	Action    string
	TargetID  string
	CreatedAt time.Time
	Details   map[string]any
}

// MetricSnapshotRecord stores one aggregated metric capture.
type MetricSnapshotRecord struct {
	ID         string
	AgentID    string
	InstanceID string
	CapturedAt time.Time
	Values     map[string]uint64
}

// EnrollmentTokenRecord stores one enrollment token and its consumption state.
type EnrollmentTokenRecord struct {
	Value        string
	FleetGroupID string
	IssuedAt     time.Time
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
	RevokedAt    *time.Time
}

// AgentCertificateRecoveryGrantRecord stores one administrator-approved certificate recovery window.
type AgentCertificateRecoveryGrantRecord struct {
	AgentID   string
	IssuedBy  string
	IssuedAt  time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
	RevokedAt *time.Time
}

// PanelSettingsRecord stores operator-managed public access settings for the panel.
type PanelSettingsRecord struct {
	HTTPPublicURL      string
	GRPCPublicEndpoint string
	UpdatedAt          time.Time
}

// CertificateAuthorityRecord stores the persisted control-plane root CA material.
type CertificateAuthorityRecord struct {
	CAPEM         string
	PrivateKeyPEM string
	UpdatedAt      time.Time
}

// ClientRecord stores one centrally managed Telemt client definition.
type ClientRecord struct {
	ID                string
	Name              string
	SecretCiphertext  string
	UserADTag         string
	Enabled           bool
	MaxTCPConns       int
	MaxUniqueIPs      int
	DataQuotaBytes    int64
	ExpirationRFC3339 string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

// ClientAssignmentRecord stores one desired rollout target for a managed client.
type ClientAssignmentRecord struct {
	ID           string
	ClientID     string
	TargetType   string
	FleetGroupID string
	AgentID      string
	CreatedAt    time.Time
}

// ClientDeploymentRecord stores the current rollout state for one client on one agent.
type ClientDeploymentRecord struct {
	ClientID         string
	AgentID          string
	DesiredOperation string
	Status           string
	LastError        string
	ConnectionLink   string
	LastAppliedAt    *time.Time
	UpdatedAt        time.Time
}
