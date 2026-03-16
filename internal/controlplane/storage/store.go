package storage

import (
	"context"
	"time"
)

// UserStore persists local control-plane user records.
type UserStore interface {
	PutUser(ctx context.Context, user UserRecord) error
	GetUserByID(ctx context.Context, userID string) (UserRecord, error)
	GetUserByUsername(ctx context.Context, username string) (UserRecord, error)
	ListUsers(ctx context.Context) ([]UserRecord, error)
}

// FleetStore persists fleet topology and discovered Telemt runtime state.
type FleetStore interface {
	PutEnvironment(ctx context.Context, environment EnvironmentRecord) error
	ListEnvironments(ctx context.Context) ([]EnvironmentRecord, error)
	PutFleetGroup(ctx context.Context, group FleetGroupRecord) error
	ListFleetGroups(ctx context.Context) ([]FleetGroupRecord, error)
	PutAgent(ctx context.Context, agent AgentRecord) error
	ListAgents(ctx context.Context) ([]AgentRecord, error)
	PutInstance(ctx context.Context, instance InstanceRecord) error
	ListInstances(ctx context.Context) ([]InstanceRecord, error)
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

// EnrollmentStore persists one-time agent enrollment tokens.
type EnrollmentStore interface {
	PutEnrollmentToken(ctx context.Context, token EnrollmentTokenRecord) error
	ListEnrollmentTokens(ctx context.Context) ([]EnrollmentTokenRecord, error)
	GetEnrollmentToken(ctx context.Context, value string) (EnrollmentTokenRecord, error)
	ConsumeEnrollmentToken(ctx context.Context, value string, consumedAt time.Time) (EnrollmentTokenRecord, error)
	RevokeEnrollmentToken(ctx context.Context, value string, revokedAt time.Time) (EnrollmentTokenRecord, error)
}

// Store aggregates the persistence capabilities required by the control-plane.
type Store interface {
	UserStore
	FleetStore
	JobStore
	AuditStore
	MetricStore
	EnrollmentStore

	Close() error
}
