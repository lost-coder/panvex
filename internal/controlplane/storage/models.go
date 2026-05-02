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
	ID         string
	UserID     string
	CreatedAt  time.Time
	// LastSeenAt is the persisted sliding-refresh timestamp (Q2.U-S-12).
	// Updated by SessionStore.TouchSession at most every
	// sessionTouchThrottle so the idle-timeout survives a restart
	// without thrashing the store on every authenticated request.
	LastSeenAt time.Time
}

// ConsumedTotpRecord stores one already-consumed TOTP code for replay
// prevention (Q2.U-S-17). The persistence layer keeps the code only
// long enough to bridge the verifier acceptance window (90s) so a CP
// restart cannot let an in-flight code be re-used.
type ConsumedTotpRecord struct {
	UserID string
	Code   string
	UsedAt time.Time
}

// LoginLockoutRecord stores the persistent login-failure state for
// one account (S7). Failures accumulates until the lockout threshold
// is reached; at that point LockedAt is set to the wall-clock time
// the lockout began. A nil LockedAt means "not currently locked".
// Username is the raw account name as submitted to /auth/login so
// the auth service can still match it after a restart — the service
// normalises to lower-case before lookup.
type LoginLockoutRecord struct {
	Username  string
	Failures  int
	LockedAt  *time.Time
	UpdatedAt time.Time
}

// AgentRevocationRecord tracks one deregistered agent whose mTLS client
// certificate may still be cryptographically valid. The record survives
// control-plane restart so a revoked agent cannot silently reconnect.
// CertExpiresAt is the cert validity cut-off; once the cert has expired
// the row is eligible for pruning because the cert can no longer
// authenticate regardless of the revocation list.
type AgentRevocationRecord struct {
	AgentID       string
	RevokedAt     time.Time
	CertExpiresAt time.Time
}

// AgentFallbackStateRecord persists when an agent first entered ME→Direct
// fallback. Cleared when MERuntimeReady returns to true. EnteredAt is the
// stable origin used to compute fallback duration for severity bucketing
// even across control-plane restarts.
type AgentFallbackStateRecord struct {
	AgentID   string
	EnteredAt time.Time
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
	AgentID            string
	DC                 int
	ObservedAt         time.Time
	AvailableEndpoints int
	AvailablePct       float64
	RequiredWriters    int
	AliveWriters       int
	CoveragePct        float64
	RTTMs              float64
	Load               float64
}

// TelemetryRuntimeUpstreamRecord stores one node's latest upstream health row.
type TelemetryRuntimeUpstreamRecord struct {
	AgentID            string
	UpstreamID         int
	ObservedAt         time.Time
	RouteKind          string
	Address            string
	Healthy            bool
	Fails              int
	EffectiveLatencyMs float64
}

// TelemetryRuntimeEventRecord stores one recent runtime event observed for a node.
type TelemetryRuntimeEventRecord struct {
	AgentID    string
	Sequence   int64
	ObservedAt time.Time
	Timestamp  time.Time
	EventType  string
	Context    string
	Severity   string
}

// TelemetryDiagnosticsCurrentRecord stores the latest slower diagnostics payloads for one node.
type TelemetryDiagnosticsCurrentRecord struct {
	AgentID             string
	ObservedAt          time.Time
	State               string
	StateReason         string
	SystemInfoJSON      string
	EffectiveLimitsJSON string
	SecurityPostureJSON string
	MinimalAllJSON      string
	MEPoolJSON          string
	DcsJSON             string
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
	AgentID   string
	ExpiresAt time.Time
	UpdatedAt time.Time
}

// FleetGroupRecord stores one fleet group in the global control-plane namespace.
//
// ID is a UUID assigned at creation and never changes. Name is an
// immutable human-readable slug (unique, used in URLs / CLI / logs).
// Label is a free-form display name the operator can edit. Description
// is free text — rendered on the detail page.
type FleetGroupRecord struct {
	ID          string
	Name        string
	Label       string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IntegrationProviderRecord stores credentials for an external
// integration backend (e.g. a Cloudflare account). A single provider
// can back FleetGroupIntegrationRecord rows across many groups.
// Config is opaque JSON — the shape is owned by the integration
// implementation and validated at install time.
type IntegrationProviderRecord struct {
	ID        string
	Kind      string
	Label     string
	Config    []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FleetGroupIntegrationRecord attaches one integration install to a
// fleet group. At most one row per (fleet_group_id, kind). ProviderID
// is nullable: some integrations embed their entire config inline and
// do not reference a shared provider.
type FleetGroupIntegrationRecord struct {
	ID           string
	FleetGroupID string
	Kind         string
	ProviderID   *string
	Config       []byte
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ReassignCounts summarises how many FK references to a fleet group
// exist (or were moved, depending on the method). Used by the
// deletion-preview endpoint and the reassignment audit entry.
type ReassignCounts struct {
	Agents            int64
	EnrollmentTokens  int64
	ClientAssignments int64
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
	// CertSerial pins the latest issued certificate's serial number so a
	// previously-issued cert (e.g. a not-yet-expired old one harvested
	// from a backup or rotation log) cannot impersonate the agent
	// (Q4.U-S-04). Hex-encoded big-endian serial.
	CertSerial string
	// CertSPKISHA256 is the SHA-256 hash of the agent's serving cert SPKI,
	// set on first successful enroll (S-02). Empty bytes mean "not yet
	// pinned"; subsequent dials must verify the served cert hashes to this
	// value via storage.UpdateAgentCertPin.
	CertSPKISHA256 []byte
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
	// PasswordMinLength is the operator-configured minimum password length.
	// Zero is sentinel for "not configured" — callers should treat it as
	// the compiled-in default (auth.DefaultPasswordMinLength).
	PasswordMinLength  int32
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
	// JobsSeconds bounds how long terminal jobs (succeeded/failed/
	// expired) live in the jobs table before the rollup loop deletes
	// them via PruneTerminalJobs (Q2.U-P-02). Zero disables job
	// pruning so existing dev fixtures keep their full history.
	JobsSeconds int `json:"jobs_seconds"`
}

// CertificateAuthorityRecord stores the persisted control-plane root CA material.
type CertificateAuthorityRecord struct {
	CAPEM         string
	PrivateKeyPEM string
	UpdatedAt     time.Time
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
// ConnectionLinks holds every Telemt-reported link for this user (one per
// tls_domain × host combination). Stored as a JSON array on disk.
type ClientDeploymentRecord struct {
	ClientID         string
	AgentID          string
	DesiredOperation string
	Status           string
	LastError        string
	ConnectionLinks  []string
	LastAppliedAt    *time.Time
	UpdatedAt        time.Time
}

// DiscoveredClientRecord stores one Telemt user found on an agent that is not managed by the panel.
type DiscoveredClientRecord struct {
	ID                 string
	AgentID            string
	ClientName         string
	Secret             string
	Status             string
	TotalOctets        uint64
	CurrentConnections int
	ActiveUniqueIPs    int
	ConnectionLinks    []string
	MaxTCPConns        int
	MaxUniqueIPs       int
	DataQuotaBytes     int64
	Expiration         string
	DiscoveredAt       time.Time
	UpdatedAt          time.Time
}

// ServerLoadPointRecord stores one aggregated runtime snapshot for timeseries.
type ServerLoadPointRecord struct {
	AgentID                string
	CapturedAt             time.Time
	CPUPctAvg              float64
	CPUPctMax              float64
	MemPctAvg              float64
	MemPctMax              float64
	DiskPctAvg             float64
	DiskPctMax             float64
	Load1M                 float64
	Load5M                 float64
	Load15M                float64
	ConnectionsAvg         int
	ConnectionsMax         int
	ConnectionsMEAvg       int
	ConnectionsDirectAvg   int
	ActiveUsersAvg         int
	ActiveUsersMax         int
	ConnectionsTotal       uint64
	ConnectionsBadTotal    uint64
	HandshakeTimeoutsTotal uint64
	DCCoverageMinPct       float64
	DCCoverageAvgPct       float64
	HealthyUpstreams       int
	TotalUpstreams         int
	NetBytesSent           uint64
	NetBytesRecv           uint64
	SampleCount            int
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

// ClientIPAggregateRecord captures the per-IP aggregate across nodes:
// the earliest first-seen and the latest last-seen for a single IP
// address, regardless of which agent reported each end of the window.
// Used by the dashboard's IP-history endpoint so the heavy aggregation
// runs in SQL instead of in CP memory.
type ClientIPAggregateRecord struct {
	IPAddress string
	FirstSeen time.Time
	LastSeen  time.Time
}

// ClientUsageRecord stores the lifetime traffic + live-gauge counters
// for one (client, agent) pair. Persisted so the in-memory
// server.clientUsage map rehydrates across restarts without losing
// accumulated totals. LastSeq is the per-agent delta cursor (rewinds
// to 1 on agent restart; the higher value wins).
type ClientUsageRecord struct {
	ClientID         string
	AgentID          string
	TrafficUsedBytes uint64
	UniqueIPsUsed    int
	ActiveTCPConns   int
	ActiveUniqueIPs  int
	LastSeq          uint64
	ObservedAt       time.Time
}

// ServerLoadHourlyRecord stores one hourly rollup of server load metrics.
type ServerLoadHourlyRecord struct {
	AgentID        string
	BucketHour     time.Time
	CPUPctAvg      float64
	CPUPctMax      float64
	MemPctAvg      float64
	MemPctMax      float64
	ConnectionsAvg float64
	ConnectionsMax int
	ActiveUsersAvg float64
	ActiveUsersMax int
	DCCoverageMin  float64
	DCCoverageAvg  float64
	SampleCount    int
}
