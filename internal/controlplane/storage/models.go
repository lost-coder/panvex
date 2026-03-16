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

// EnvironmentRecord stores one fleet environment definition.
type EnvironmentRecord struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// FleetGroupRecord stores one fleet group that belongs to an environment.
type FleetGroupRecord struct {
	ID            string
	EnvironmentID string
	Name          string
	CreatedAt     time.Time
}

// AgentRecord stores one enrolled host agent snapshot.
type AgentRecord struct {
	ID            string
	NodeName      string
	EnvironmentID string
	FleetGroupID  string
	Version       string
	ReadOnly      bool
	LastSeenAt    time.Time
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
}

// JobTargetRecord stores delivery and result state for one job target.
type JobTargetRecord struct {
	JobID      string
	AgentID    string
	Status     string
	ResultText string
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
	Value         string
	EnvironmentID string
	FleetGroupID  string
	IssuedAt      time.Time
	ExpiresAt     time.Time
	ConsumedAt    *time.Time
	RevokedAt     *time.Time
}
