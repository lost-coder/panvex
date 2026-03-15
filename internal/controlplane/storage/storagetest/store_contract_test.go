package storagetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

func TestStoreContractWithMemoryStore(t *testing.T) {
	RunStoreContract(t, func(t *testing.T) storage.Store {
		t.Helper()
		return newMemoryStore()
	})
}

type memoryStore struct {
	users              map[string]storage.UserRecord
	usernames          map[string]string
	environments       map[string]storage.EnvironmentRecord
	fleetGroups        map[string]storage.FleetGroupRecord
	agents             map[string]storage.AgentRecord
	instances          map[string]storage.InstanceRecord
	jobs               map[string]storage.JobRecord
	jobsByKey          map[string]string
	jobTargets         map[string]storage.JobTargetRecord
	auditEvents        []storage.AuditEventRecord
	metricSnapshots    []storage.MetricSnapshotRecord
	enrollmentTokens   map[string]storage.EnrollmentTokenRecord
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		users:            make(map[string]storage.UserRecord),
		usernames:        make(map[string]string),
		environments:     make(map[string]storage.EnvironmentRecord),
		fleetGroups:      make(map[string]storage.FleetGroupRecord),
		agents:           make(map[string]storage.AgentRecord),
		instances:        make(map[string]storage.InstanceRecord),
		jobs:             make(map[string]storage.JobRecord),
		jobsByKey:        make(map[string]string),
		jobTargets:       make(map[string]storage.JobTargetRecord),
		auditEvents:      make([]storage.AuditEventRecord, 0),
		metricSnapshots:  make([]storage.MetricSnapshotRecord, 0),
		enrollmentTokens: make(map[string]storage.EnrollmentTokenRecord),
	}
}

func (s *memoryStore) Close() error {
	return nil
}

func (s *memoryStore) PutUser(_ context.Context, user storage.UserRecord) error {
	s.users[user.ID] = user
	s.usernames[user.Username] = user.ID
	return nil
}

func (s *memoryStore) GetUserByID(_ context.Context, userID string) (storage.UserRecord, error) {
	user, ok := s.users[userID]
	if !ok {
		return storage.UserRecord{}, storage.ErrNotFound
	}

	return user, nil
}

func (s *memoryStore) GetUserByUsername(_ context.Context, username string) (storage.UserRecord, error) {
	userID, ok := s.usernames[username]
	if !ok {
		return storage.UserRecord{}, storage.ErrNotFound
	}

	return s.users[userID], nil
}

func (s *memoryStore) ListUsers(_ context.Context) ([]storage.UserRecord, error) {
	result := make([]storage.UserRecord, 0, len(s.users))
	for _, user := range s.users {
		result = append(result, user)
	}

	return result, nil
}

func (s *memoryStore) PutEnvironment(_ context.Context, environment storage.EnvironmentRecord) error {
	s.environments[environment.ID] = environment
	return nil
}

func (s *memoryStore) ListEnvironments(_ context.Context) ([]storage.EnvironmentRecord, error) {
	result := make([]storage.EnvironmentRecord, 0, len(s.environments))
	for _, environment := range s.environments {
		result = append(result, environment)
	}

	return result, nil
}

func (s *memoryStore) PutFleetGroup(_ context.Context, group storage.FleetGroupRecord) error {
	s.fleetGroups[group.ID] = group
	return nil
}

func (s *memoryStore) ListFleetGroups(_ context.Context) ([]storage.FleetGroupRecord, error) {
	result := make([]storage.FleetGroupRecord, 0, len(s.fleetGroups))
	for _, group := range s.fleetGroups {
		result = append(result, group)
	}

	return result, nil
}

func (s *memoryStore) PutAgent(_ context.Context, agent storage.AgentRecord) error {
	s.agents[agent.ID] = agent
	return nil
}

func (s *memoryStore) ListAgents(_ context.Context) ([]storage.AgentRecord, error) {
	result := make([]storage.AgentRecord, 0, len(s.agents))
	for _, agent := range s.agents {
		result = append(result, agent)
	}

	return result, nil
}

func (s *memoryStore) PutInstance(_ context.Context, instance storage.InstanceRecord) error {
	s.instances[instance.ID] = instance
	return nil
}

func (s *memoryStore) ListInstances(_ context.Context) ([]storage.InstanceRecord, error) {
	result := make([]storage.InstanceRecord, 0, len(s.instances))
	for _, instance := range s.instances {
		result = append(result, instance)
	}

	return result, nil
}

func (s *memoryStore) PutJob(_ context.Context, job storage.JobRecord) error {
	s.jobs[job.ID] = job
	s.jobsByKey[job.IdempotencyKey] = job.ID
	return nil
}

func (s *memoryStore) GetJobByIdempotencyKey(_ context.Context, idempotencyKey string) (storage.JobRecord, error) {
	jobID, ok := s.jobsByKey[idempotencyKey]
	if !ok {
		return storage.JobRecord{}, storage.ErrNotFound
	}

	return s.jobs[jobID], nil
}

func (s *memoryStore) ListJobs(_ context.Context) ([]storage.JobRecord, error) {
	result := make([]storage.JobRecord, 0, len(s.jobs))
	for _, job := range s.jobs {
		result = append(result, job)
	}

	return result, nil
}

func (s *memoryStore) PutJobTarget(_ context.Context, target storage.JobTargetRecord) error {
	s.jobTargets[fmt.Sprintf("%s/%s", target.JobID, target.AgentID)] = target
	return nil
}

func (s *memoryStore) ListJobTargets(_ context.Context, jobID string) ([]storage.JobTargetRecord, error) {
	result := make([]storage.JobTargetRecord, 0)
	for _, target := range s.jobTargets {
		if target.JobID == jobID {
			result = append(result, target)
		}
	}

	return result, nil
}

func (s *memoryStore) AppendAuditEvent(_ context.Context, event storage.AuditEventRecord) error {
	s.auditEvents = append(s.auditEvents, event)
	return nil
}

func (s *memoryStore) ListAuditEvents(_ context.Context) ([]storage.AuditEventRecord, error) {
	return append([]storage.AuditEventRecord(nil), s.auditEvents...), nil
}

func (s *memoryStore) AppendMetricSnapshot(_ context.Context, snapshot storage.MetricSnapshotRecord) error {
	s.metricSnapshots = append(s.metricSnapshots, snapshot)
	return nil
}

func (s *memoryStore) ListMetricSnapshots(_ context.Context) ([]storage.MetricSnapshotRecord, error) {
	return append([]storage.MetricSnapshotRecord(nil), s.metricSnapshots...), nil
}

func (s *memoryStore) PutEnrollmentToken(_ context.Context, token storage.EnrollmentTokenRecord) error {
	s.enrollmentTokens[token.Value] = token
	return nil
}

func (s *memoryStore) ListEnrollmentTokens(_ context.Context) ([]storage.EnrollmentTokenRecord, error) {
	result := make([]storage.EnrollmentTokenRecord, 0, len(s.enrollmentTokens))
	for _, token := range s.enrollmentTokens {
		result = append(result, token)
	}

	return result, nil
}

func (s *memoryStore) GetEnrollmentToken(_ context.Context, value string) (storage.EnrollmentTokenRecord, error) {
	token, ok := s.enrollmentTokens[value]
	if !ok {
		return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
	}

	return token, nil
}

func (s *memoryStore) ConsumeEnrollmentToken(_ context.Context, value string, consumedAt time.Time) (storage.EnrollmentTokenRecord, error) {
	token, ok := s.enrollmentTokens[value]
	if !ok {
		return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
	}

	if token.ConsumedAt != nil {
		return storage.EnrollmentTokenRecord{}, storage.ErrConflict
	}

	token.ConsumedAt = &consumedAt
	s.enrollmentTokens[value] = token

	return token, nil
}
