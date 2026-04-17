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

// SessionRecord stores one authenticated user session.
type SessionRecord struct {
	ID        string
	UserID    string
	CreatedAt time.Time
}

// AgentRevocationRecord tracks one deregistered agent whose mTLS client
// certificate may still be cryptographically valid. The record survives
// control-plane restart so a revoked agent cannot silently reconnect.
// CertExpiresAt is the cert validity cut-off; once the cert has expired
// the row is eligible for pruning because the cert can no longer
// authenticate regardless of the revocation list.
type AgentRevocationRecord struct {
	AgentID        string
	RevokedAt      time.Time
	CertExpiresAt  time.Time
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
	DcsJSON              string
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
	ID            string
	NodeName      string
	FleetGroupID  string
	Version       string
	ReadOnly      bool
	LastSeenAt    time.Time
	CertIssuedAt  *time.Time
	CertExpiresAt *time.Time
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

// RetentionSettingsRecord stores operator-managed timeseries/event retention
// windows. Persisted as an opaque JSON blob in panel_settings.retention_json
// so adding new retention knobs never needs another migration.
type RetentionSettingsRecord struct {
	TSRawSeconds          int `json:"ts_raw_seconds"`
	TSHourlySeconds       int `json:"ts_hourly_seconds"`
	TSDCSeconds           int `json:"ts_dc_seconds"`
	IPHistorySeconds      int `json:"ip_history_seconds"`
	EventSeconds          int `json:"event_history_seconds"`
	AuditEventSeconds     int `json:"audit_event_seconds"`
	MetricSnapshotSeconds int `json:"metric_snapshot_seconds"`
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

// DiscoveredClientRecord stores one Telemt user found on an agent that is not managed by the panel.
type DiscoveredClientRecord struct {
	ID                string
	AgentID           string
	ClientName        string
	Secret            string
	Status            string
	TotalOctets       uint64
	CurrentConnections int
	ActiveUniqueIPs   int
	ConnectionLink    string
	MaxTCPConns       int
	MaxUniqueIPs      int
	DataQuotaBytes    int64
	Expiration        string
	DiscoveredAt      time.Time
	UpdatedAt         time.Time
}

// ServerLoadPointRecord stores one aggregated runtime snapshot for timeseries.
type ServerLoadPointRecord struct {
	AgentID               string
	CapturedAt            time.Time
	CPUPctAvg             float64
	CPUPctMax             float64
	MemPctAvg             float64
	MemPctMax             float64
	DiskPctAvg            float64
	DiskPctMax            float64
	Load1M                float64
	Load5M                float64
	Load15M               float64
	ConnectionsAvg        int
	ConnectionsMax        int
	ConnectionsMEAvg      int
	ConnectionsDirectAvg  int
	ActiveUsersAvg        int
	ActiveUsersMax        int
	ConnectionsTotal      uint64
	ConnectionsBadTotal   uint64
	HandshakeTimeoutsTotal uint64
	DCCoverageMinPct      float64
	DCCoverageAvgPct      float64
	HealthyUpstreams      int
	TotalUpstreams        int
	NetBytesSent          uint64
	NetBytesRecv          uint64
	SampleCount           int
}

// DCHealthPointRecord stores one aggregated DC health snapshot.
type DCHealthPointRecord struct {
	AgentID         string
	CapturedAt      time.Time
	DC              int
	CoveragePctAvg  float64
	CoveragePctMin  float64
	RTTMsAvg        float64
	RTTMsMax        float64
	AliveWritersMin int
	RequiredWriters int
	LoadMax         int
	SampleCount     int
}

// ClientIPHistoryRecord stores one unique IP seen for a client on an agent.
type ClientIPHistoryRecord struct {
	AgentID   string
	ClientID  string
	IPAddress string
	FirstSeen time.Time
	LastSeen  time.Time
}

// ServerLoadHourlyRecord stores one hourly rollup of server load metrics.
type ServerLoadHourlyRecord struct {
	AgentID         string
	BucketHour      time.Time
	CPUPctAvg       float64
	CPUPctMax       float64
	MemPctAvg       float64
	MemPctMax       float64
	ConnectionsAvg  float64
	ConnectionsMax  int
	ActiveUsersAvg  float64
	ActiveUsersMax  int
	DCCoverageMin   float64
	DCCoverageAvg   float64
	SampleCount     int
}
