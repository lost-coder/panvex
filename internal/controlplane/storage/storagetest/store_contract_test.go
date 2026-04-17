package storagetest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
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
	telemetryRuntimeCurrent map[string]storage.TelemetryRuntimeCurrentRecord
	telemetryRuntimeDCs map[string][]storage.TelemetryRuntimeDCRecord
	telemetryRuntimeUpstreams map[string][]storage.TelemetryRuntimeUpstreamRecord
	telemetryRuntimeEvents map[string][]storage.TelemetryRuntimeEventRecord
	telemetryDiagnosticsCurrent map[string]storage.TelemetryDiagnosticsCurrentRecord
	telemetrySecurityCurrent map[string]storage.TelemetrySecurityInventoryCurrentRecord
	telemetryDetailBoosts map[string]storage.TelemetryDetailBoostRecord
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
	discoveredClients  map[string]storage.DiscoveredClientRecord
	sessions           map[string]storage.SessionRecord
	agentRevocations   map[string]storage.AgentRevocationRecord
	panelSettings      *storage.PanelSettingsRecord
	updateSettings     json.RawMessage
	updateState        json.RawMessage
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
		telemetryRuntimeCurrent: make(map[string]storage.TelemetryRuntimeCurrentRecord),
		telemetryRuntimeDCs: make(map[string][]storage.TelemetryRuntimeDCRecord),
		telemetryRuntimeUpstreams: make(map[string][]storage.TelemetryRuntimeUpstreamRecord),
		telemetryRuntimeEvents: make(map[string][]storage.TelemetryRuntimeEventRecord),
		telemetryDiagnosticsCurrent: make(map[string]storage.TelemetryDiagnosticsCurrentRecord),
		telemetrySecurityCurrent: make(map[string]storage.TelemetrySecurityInventoryCurrentRecord),
		telemetryDetailBoosts: make(map[string]storage.TelemetryDetailBoostRecord),
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
		discoveredClients: make(map[string]storage.DiscoveredClientRecord),
		sessions:          make(map[string]storage.SessionRecord),
		agentRevocations:  make(map[string]storage.AgentRevocationRecord),
	}
}

func (s *memoryStore) Ping(_ context.Context) error {
	return nil
}

func (s *memoryStore) Close() error {
	return nil
}

func (s *memoryStore) PutAgentRevocation(_ context.Context, rec storage.AgentRevocationRecord) error {
	existing, ok := s.agentRevocations[rec.AgentID]
	// Mirror the SQL upsert: cert_expires_at is max-merged so we don't shrink
	// the revocation window if a caller passes an older expiry.
	if ok && existing.CertExpiresAt.After(rec.CertExpiresAt) {
		rec.CertExpiresAt = existing.CertExpiresAt
	}
	s.agentRevocations[rec.AgentID] = rec
	return nil
}

func (s *memoryStore) ListAgentRevocations(_ context.Context) ([]storage.AgentRevocationRecord, error) {
	out := make([]storage.AgentRevocationRecord, 0, len(s.agentRevocations))
	for _, r := range s.agentRevocations {
		out = append(out, r)
	}
	return out, nil
}

func (s *memoryStore) DeleteExpiredAgentRevocations(_ context.Context, before time.Time) (int64, error) {
	var removed int64
	for id, rec := range s.agentRevocations {
		if rec.CertExpiresAt.Before(before) {
			delete(s.agentRevocations, id)
			removed++
		}
	}
	return removed, nil
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
			HelpMode: "basic",
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

func (s *memoryStore) DeleteAgent(_ context.Context, agentID string) error {
	if _, ok := s.agents[agentID]; !ok {
		return storage.ErrNotFound
	}
	delete(s.agents, agentID)
	return nil
}

func (s *memoryStore) UpdateAgentNodeName(_ context.Context, agentID string, nodeName string) error {
	agent, ok := s.agents[agentID]
	if !ok {
		return storage.ErrNotFound
	}
	agent.NodeName = nodeName
	s.agents[agentID] = agent
	return nil
}

func (s *memoryStore) DeleteInstancesByAgent(_ context.Context, agentID string) error {
	for id, inst := range s.instances {
		if inst.AgentID == agentID {
			delete(s.instances, id)
		}
	}
	return nil
}

func (s *memoryStore) PutInstance(_ context.Context, instance storage.InstanceRecord) error {
	s.instances[instance.ID] = instance
	return nil
}

func (s *memoryStore) PutTelemetryRuntimeCurrent(_ context.Context, record storage.TelemetryRuntimeCurrentRecord) error {
	s.telemetryRuntimeCurrent[record.AgentID] = record
	return nil
}

func (s *memoryStore) GetTelemetryRuntimeCurrent(_ context.Context, agentID string) (storage.TelemetryRuntimeCurrentRecord, error) {
	record, ok := s.telemetryRuntimeCurrent[agentID]
	if !ok {
		return storage.TelemetryRuntimeCurrentRecord{}, storage.ErrNotFound
	}

	return record, nil
}

func (s *memoryStore) ListTelemetryRuntimeCurrent(_ context.Context) ([]storage.TelemetryRuntimeCurrentRecord, error) {
	result := make([]storage.TelemetryRuntimeCurrentRecord, 0, len(s.telemetryRuntimeCurrent))
	for _, record := range s.telemetryRuntimeCurrent {
		result = append(result, record)
	}

	return result, nil
}

func (s *memoryStore) ReplaceTelemetryRuntimeDCs(_ context.Context, agentID string, records []storage.TelemetryRuntimeDCRecord) error {
	s.telemetryRuntimeDCs[agentID] = append([]storage.TelemetryRuntimeDCRecord(nil), records...)
	return nil
}

func (s *memoryStore) ListTelemetryRuntimeDCs(_ context.Context, agentID string) ([]storage.TelemetryRuntimeDCRecord, error) {
	return append([]storage.TelemetryRuntimeDCRecord(nil), s.telemetryRuntimeDCs[agentID]...), nil
}

func (s *memoryStore) ReplaceTelemetryRuntimeUpstreams(_ context.Context, agentID string, records []storage.TelemetryRuntimeUpstreamRecord) error {
	s.telemetryRuntimeUpstreams[agentID] = append([]storage.TelemetryRuntimeUpstreamRecord(nil), records...)
	return nil
}

func (s *memoryStore) ListTelemetryRuntimeUpstreams(_ context.Context, agentID string) ([]storage.TelemetryRuntimeUpstreamRecord, error) {
	return append([]storage.TelemetryRuntimeUpstreamRecord(nil), s.telemetryRuntimeUpstreams[agentID]...), nil
}

func (s *memoryStore) AppendTelemetryRuntimeEvents(_ context.Context, agentID string, records []storage.TelemetryRuntimeEventRecord) error {
	s.telemetryRuntimeEvents[agentID] = append(s.telemetryRuntimeEvents[agentID], records...)
	return nil
}

func (s *memoryStore) ListTelemetryRuntimeEvents(_ context.Context, agentID string, limit int) ([]storage.TelemetryRuntimeEventRecord, error) {
	records := append([]storage.TelemetryRuntimeEventRecord(nil), s.telemetryRuntimeEvents[agentID]...)
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}

	return records, nil
}

func (s *memoryStore) PruneTelemetryRuntimeEvents(_ context.Context, olderThan time.Time) (int64, error) {
	var pruned int64
	for agentID, events := range s.telemetryRuntimeEvents {
		var kept []storage.TelemetryRuntimeEventRecord
		for _, e := range events {
			if !e.Timestamp.Before(olderThan) {
				kept = append(kept, e)
			} else {
				pruned++
			}
		}
		s.telemetryRuntimeEvents[agentID] = kept
	}
	return pruned, nil
}

func (s *memoryStore) PutTelemetryDiagnosticsCurrent(_ context.Context, record storage.TelemetryDiagnosticsCurrentRecord) error {
	s.telemetryDiagnosticsCurrent[record.AgentID] = record
	return nil
}

func (s *memoryStore) GetTelemetryDiagnosticsCurrent(_ context.Context, agentID string) (storage.TelemetryDiagnosticsCurrentRecord, error) {
	record, ok := s.telemetryDiagnosticsCurrent[agentID]
	if !ok {
		return storage.TelemetryDiagnosticsCurrentRecord{}, storage.ErrNotFound
	}

	return record, nil
}

func (s *memoryStore) PutTelemetrySecurityInventoryCurrent(_ context.Context, record storage.TelemetrySecurityInventoryCurrentRecord) error {
	s.telemetrySecurityCurrent[record.AgentID] = record
	return nil
}

func (s *memoryStore) GetTelemetrySecurityInventoryCurrent(_ context.Context, agentID string) (storage.TelemetrySecurityInventoryCurrentRecord, error) {
	record, ok := s.telemetrySecurityCurrent[agentID]
	if !ok {
		return storage.TelemetrySecurityInventoryCurrentRecord{}, storage.ErrNotFound
	}

	return record, nil
}

func (s *memoryStore) PutTelemetryDetailBoost(_ context.Context, record storage.TelemetryDetailBoostRecord) error {
	s.telemetryDetailBoosts[record.AgentID] = record
	return nil
}

func (s *memoryStore) ListTelemetryDetailBoosts(_ context.Context) ([]storage.TelemetryDetailBoostRecord, error) {
	result := make([]storage.TelemetryDetailBoostRecord, 0, len(s.telemetryDetailBoosts))
	for _, record := range s.telemetryDetailBoosts {
		result = append(result, record)
	}

	return result, nil
}

func (s *memoryStore) DeleteTelemetryDetailBoost(_ context.Context, agentID string) error {
	delete(s.telemetryDetailBoosts, agentID)
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

func (s *memoryStore) ListAuditEvents(_ context.Context, limit int) ([]storage.AuditEventRecord, error) {
	events := append([]storage.AuditEventRecord(nil), s.auditEvents...)
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
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

func (s *memoryStore) PutUpdateSettings(_ context.Context, data json.RawMessage) error {
	s.updateSettings = append(json.RawMessage(nil), data...)
	return nil
}

func (s *memoryStore) GetUpdateSettings(_ context.Context) (json.RawMessage, error) {
	if s.updateSettings == nil {
		return nil, nil
	}
	return append(json.RawMessage(nil), s.updateSettings...), nil
}

func (s *memoryStore) PutUpdateState(_ context.Context, data json.RawMessage) error {
	s.updateState = append(json.RawMessage(nil), data...)
	return nil
}

func (s *memoryStore) GetUpdateState(_ context.Context) (json.RawMessage, error) {
	if s.updateState == nil {
		return nil, nil
	}
	return append(json.RawMessage(nil), s.updateState...), nil
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

func (s *memoryStore) PutDiscoveredClient(_ context.Context, record storage.DiscoveredClientRecord) error {
	// Match UPSERT behavior: key by (agent_id, client_name).
	for id, existing := range s.discoveredClients {
		if existing.AgentID == record.AgentID && existing.ClientName == record.ClientName {
			if existing.Status == "ignored" {
				record.Status = existing.Status
			}
			record.ID = id
			s.discoveredClients[id] = record
			return nil
		}
	}
	s.discoveredClients[record.ID] = record
	return nil
}

func (s *memoryStore) ListDiscoveredClients(_ context.Context) ([]storage.DiscoveredClientRecord, error) {
	result := make([]storage.DiscoveredClientRecord, 0, len(s.discoveredClients))
	for _, r := range s.discoveredClients {
		result = append(result, r)
	}
	return result, nil
}

func (s *memoryStore) ListDiscoveredClientsByAgent(_ context.Context, agentID string) ([]storage.DiscoveredClientRecord, error) {
	result := make([]storage.DiscoveredClientRecord, 0)
	for _, r := range s.discoveredClients {
		if r.AgentID == agentID {
			result = append(result, r)
		}
	}
	return result, nil
}

func (s *memoryStore) GetDiscoveredClient(_ context.Context, id string) (storage.DiscoveredClientRecord, error) {
	r, ok := s.discoveredClients[id]
	if !ok {
		return storage.DiscoveredClientRecord{}, storage.ErrNotFound
	}
	return r, nil
}

func (s *memoryStore) UpdateDiscoveredClientStatus(_ context.Context, id string, status string, updatedAt time.Time) error {
	r, ok := s.discoveredClients[id]
	if !ok {
		return storage.ErrNotFound
	}
	r.Status = status
	r.UpdatedAt = updatedAt
	s.discoveredClients[id] = r
	return nil
}

func (s *memoryStore) DeleteDiscoveredClient(_ context.Context, id string) error {
	if _, ok := s.discoveredClients[id]; !ok {
		return storage.ErrNotFound
	}
	delete(s.discoveredClients, id)
	return nil
}

func (s *memoryStore) AppendServerLoadPoint(_ context.Context, _ storage.ServerLoadPointRecord) error {
	return nil
}

func (s *memoryStore) ListServerLoadPoints(_ context.Context, _ string, _ time.Time, _ time.Time) ([]storage.ServerLoadPointRecord, error) {
	return nil, nil
}

func (s *memoryStore) PruneServerLoadPoints(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (s *memoryStore) AppendDCHealthPoint(_ context.Context, _ storage.DCHealthPointRecord) error {
	return nil
}

func (s *memoryStore) ListDCHealthPoints(_ context.Context, _ string, _ time.Time, _ time.Time) ([]storage.DCHealthPointRecord, error) {
	return nil, nil
}

func (s *memoryStore) PruneDCHealthPoints(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (s *memoryStore) UpsertClientIPHistory(_ context.Context, _ storage.ClientIPHistoryRecord) error {
	return nil
}

func (s *memoryStore) ListClientIPHistory(_ context.Context, _ string, _ time.Time, _ time.Time) ([]storage.ClientIPHistoryRecord, error) {
	return nil, nil
}

func (s *memoryStore) CountUniqueClientIPs(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (s *memoryStore) PruneClientIPHistory(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (s *memoryStore) RollupServerLoadHourly(_ context.Context, _ time.Time) error {
	return nil
}

func (s *memoryStore) ListServerLoadHourly(_ context.Context, _ string, _ time.Time, _ time.Time) ([]storage.ServerLoadHourlyRecord, error) {
	return nil, nil
}

func (s *memoryStore) PruneServerLoadHourly(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (s *memoryStore) PutSession(_ context.Context, session storage.SessionRecord) error {
	s.sessions[session.ID] = session
	return nil
}

func (s *memoryStore) GetSession(_ context.Context, sessionID string) (storage.SessionRecord, error) {
	session, ok := s.sessions[sessionID]
	if !ok {
		return storage.SessionRecord{}, storage.ErrNotFound
	}
	return session, nil
}

func (s *memoryStore) DeleteSession(_ context.Context, sessionID string) error {
	if _, ok := s.sessions[sessionID]; !ok {
		return storage.ErrNotFound
	}
	delete(s.sessions, sessionID)
	return nil
}

func (s *memoryStore) ListSessions(_ context.Context) ([]storage.SessionRecord, error) {
	result := make([]storage.SessionRecord, 0, len(s.sessions))
	for _, session := range s.sessions {
		result = append(result, session)
	}
	return result, nil
}

func (s *memoryStore) DeleteExpiredSessions(_ context.Context, before time.Time) error {
	for id, session := range s.sessions {
		if session.CreatedAt.Before(before) {
			delete(s.sessions, id)
		}
	}
	return nil
}
