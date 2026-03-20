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

// PanelSettingsRecord stores operator-managed public access settings for the panel.
type PanelSettingsRecord struct {
	HTTPPublicURL      string
	GRPCPublicEndpoint string
	UpdatedAt          time.Time
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
