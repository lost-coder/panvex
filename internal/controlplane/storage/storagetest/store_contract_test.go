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
	userAppearance     map[string]storage.UserAppearanceRecord
	fleetGroups        map[string]storage.FleetGroupRecord
	agents             map[string]storage.AgentRecord
	instances          map[string]storage.InstanceRecord
	clients            map[string]storage.ClientRecord
	clientAssignments  map[string]storage.ClientAssignmentRecord
	clientDeployments  map[string]storage.ClientDeploymentRecord
	jobs               map[string]storage.JobRecord
	jobsByKey          map[string]string
	jobTargets         map[string]storage.JobTargetRecord
	auditEvents        []storage.AuditEventRecord
	metricSnapshots    []storage.MetricSnapshotRecord
	enrollmentTokens   map[string]storage.EnrollmentTokenRecord
	agentCertificateRecoveryGrants map[string]storage.AgentCertificateRecoveryGrantRecord
	panelSettings      *storage.PanelSettingsRecord
	certificateAuthority *storage.CertificateAuthorityRecord
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		users:            make(map[string]storage.UserRecord),
		usernames:        make(map[string]string),
		userAppearance:   make(map[string]storage.UserAppearanceRecord),
		fleetGroups:      make(map[string]storage.FleetGroupRecord),
		agents:           make(map[string]storage.AgentRecord),
		instances:        make(map[string]storage.InstanceRecord),
		clients:          make(map[string]storage.ClientRecord),
		clientAssignments: make(map[string]storage.ClientAssignmentRecord),
		clientDeployments: make(map[string]storage.ClientDeploymentRecord),
		jobs:             make(map[string]storage.JobRecord),
		jobsByKey:        make(map[string]string),
		jobTargets:       make(map[string]storage.JobTargetRecord),
		auditEvents:      make([]storage.AuditEventRecord, 0),
		metricSnapshots:  make([]storage.MetricSnapshotRecord, 0),
		enrollmentTokens: make(map[string]storage.EnrollmentTokenRecord),
		agentCertificateRecoveryGrants: make(map[string]storage.AgentCertificateRecoveryGrantRecord),
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

func (s *memoryStore) DeleteUser(_ context.Context, userID string) error {
	user, ok := s.users[userID]
	if !ok {
		return storage.ErrNotFound
	}

	delete(s.users, userID)
	delete(s.usernames, user.Username)
	delete(s.userAppearance, userID)
	return nil
}

func (s *memoryStore) ListUsers(_ context.Context) ([]storage.UserRecord, error) {
	result := make([]storage.UserRecord, 0, len(s.users))
	for _, user := range s.users {
		result = append(result, user)
	}

	return result, nil
}

func (s *memoryStore) PutUserAppearance(_ context.Context, appearance storage.UserAppearanceRecord) error {
	s.userAppearance[appearance.UserID] = appearance
	return nil
}

func (s *memoryStore) GetUserAppearance(_ context.Context, userID string) (storage.UserAppearanceRecord, error) {
	appearance, ok := s.userAppearance[userID]
	if !ok {
		return storage.UserAppearanceRecord{
			UserID:  userID,
			Theme:   "system",
			Density: "comfortable",
		}, nil
	}

	return appearance, nil
}

func (s *memoryStore) ListUserAppearances(_ context.Context) ([]storage.UserAppearanceRecord, error) {
	result := make([]storage.UserAppearanceRecord, 0, len(s.userAppearance))
	for _, appearance := range s.userAppearance {
		result = append(result, appearance)
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

func (s *memoryStore) PutClient(_ context.Context, client storage.ClientRecord) error {
	s.clients[client.ID] = client
	return nil
}

func (s *memoryStore) GetClientByID(_ context.Context, clientID string) (storage.ClientRecord, error) {
	client, ok := s.clients[clientID]
	if !ok {
		return storage.ClientRecord{}, storage.ErrNotFound
	}

	return client, nil
}

func (s *memoryStore) ListClients(_ context.Context) ([]storage.ClientRecord, error) {
	result := make([]storage.ClientRecord, 0, len(s.clients))
	for _, client := range s.clients {
		result = append(result, client)
	}

	return result, nil
}

func (s *memoryStore) PutClientAssignment(_ context.Context, assignment storage.ClientAssignmentRecord) error {
	s.clientAssignments[assignment.ID] = assignment
	return nil
}

func (s *memoryStore) DeleteClientAssignments(_ context.Context, clientID string) error {
	for id, assignment := range s.clientAssignments {
		if assignment.ClientID == clientID {
			delete(s.clientAssignments, id)
		}
	}

	return nil
}

func (s *memoryStore) ListClientAssignments(_ context.Context, clientID string) ([]storage.ClientAssignmentRecord, error) {
	result := make([]storage.ClientAssignmentRecord, 0)
	for _, assignment := range s.clientAssignments {
		if assignment.ClientID == clientID {
			result = append(result, assignment)
		}
	}

	return result, nil
}

func (s *memoryStore) PutClientDeployment(_ context.Context, deployment storage.ClientDeploymentRecord) error {
	s.clientDeployments[fmt.Sprintf("%s/%s", deployment.ClientID, deployment.AgentID)] = deployment
	return nil
}

func (s *memoryStore) ListClientDeployments(_ context.Context, clientID string) ([]storage.ClientDeploymentRecord, error) {
	result := make([]storage.ClientDeploymentRecord, 0)
	for _, deployment := range s.clientDeployments {
		if deployment.ClientID == clientID {
			result = append(result, deployment)
		}
	}

	return result, nil
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

func (s *memoryStore) PutPanelSettings(_ context.Context, settings storage.PanelSettingsRecord) error {
	copySettings := settings
	s.panelSettings = &copySettings
	return nil
}

func (s *memoryStore) GetPanelSettings(_ context.Context) (storage.PanelSettingsRecord, error) {
	if s.panelSettings == nil {
		return storage.PanelSettingsRecord{}, storage.ErrNotFound
	}

	return *s.panelSettings, nil
}

func (s *memoryStore) PutCertificateAuthority(_ context.Context, authority storage.CertificateAuthorityRecord) error {
	copyAuthority := authority
	s.certificateAuthority = &copyAuthority
	return nil
}

func (s *memoryStore) GetCertificateAuthority(_ context.Context) (storage.CertificateAuthorityRecord, error) {
	if s.certificateAuthority == nil {
		return storage.CertificateAuthorityRecord{}, storage.ErrNotFound
	}

	return *s.certificateAuthority, nil
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

func (s *memoryStore) RevokeEnrollmentToken(_ context.Context, value string, revokedAt time.Time) (storage.EnrollmentTokenRecord, error) {
	token, ok := s.enrollmentTokens[value]
	if !ok {
		return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
	}

	if token.RevokedAt != nil || token.ConsumedAt != nil {
		return token, nil
	}

	token.RevokedAt = &revokedAt
	s.enrollmentTokens[value] = token

	return token, nil
}

func (s *memoryStore) PutAgentCertificateRecoveryGrant(_ context.Context, grant storage.AgentCertificateRecoveryGrantRecord) error {
	s.agentCertificateRecoveryGrants[grant.AgentID] = grant
	return nil
}

func (s *memoryStore) ListAgentCertificateRecoveryGrants(_ context.Context) ([]storage.AgentCertificateRecoveryGrantRecord, error) {
	result := make([]storage.AgentCertificateRecoveryGrantRecord, 0, len(s.agentCertificateRecoveryGrants))
	for _, grant := range s.agentCertificateRecoveryGrants {
		result = append(result, grant)
	}

	return result, nil
}

func (s *memoryStore) GetAgentCertificateRecoveryGrant(_ context.Context, agentID string) (storage.AgentCertificateRecoveryGrantRecord, error) {
	grant, ok := s.agentCertificateRecoveryGrants[agentID]
	if !ok {
		return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
	}

	return grant, nil
}

func (s *memoryStore) UseAgentCertificateRecoveryGrant(_ context.Context, agentID string, usedAt time.Time) (storage.AgentCertificateRecoveryGrantRecord, error) {
	grant, ok := s.agentCertificateRecoveryGrants[agentID]
	if !ok {
		return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
	}
	if grant.UsedAt != nil || grant.RevokedAt != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrConflict
	}

	grant.UsedAt = &usedAt
	s.agentCertificateRecoveryGrants[agentID] = grant
	return grant, nil
}

func (s *memoryStore) RevokeAgentCertificateRecoveryGrant(_ context.Context, agentID string, revokedAt time.Time) (storage.AgentCertificateRecoveryGrantRecord, error) {
	grant, ok := s.agentCertificateRecoveryGrants[agentID]
	if !ok {
		return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
	}
	if grant.RevokedAt != nil || grant.UsedAt != nil {
		return grant, nil
	}

	grant.RevokedAt = &revokedAt
	s.agentCertificateRecoveryGrants[agentID] = grant
	return grant, nil
}
