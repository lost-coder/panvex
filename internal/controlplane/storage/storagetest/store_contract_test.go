package storagetest

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func TestStoreContractWithMemoryStore(t *testing.T) {
	RunStoreContract(t, func(t *testing.T) storage.MigrationStore {
		t.Helper()
		return newMemoryStore()
	})
}

type memoryStore struct {
	// txMu serializes Transact callbacks so the contract's
	// "concurrent writers" test observes the contract's exclusivity
	// guarantee (each Transact runs atomically w.r.t. other Transacts)
	// without introducing per-field locking across the whole store.
	txMu                           sync.Mutex
	users                          map[string]storage.UserRecord
	usernames                      map[string]string
	userAppearance                 map[string]storage.UserAppearanceRecord
	fleetGroups                    map[string]storage.FleetGroupRecord
	agents                         map[string]storage.AgentRecord
	instances                      map[string]storage.InstanceRecord
	telemetryRuntimeCurrent        map[string]storage.TelemetryRuntimeCurrentRecord
	telemetryRuntimeDCs            map[string][]storage.TelemetryRuntimeDCRecord
	telemetryRuntimeUpstreams      map[string][]storage.TelemetryRuntimeUpstreamRecord
	telemetryRuntimeEvents         map[string][]storage.TelemetryRuntimeEventRecord
	telemetryDiagnosticsCurrent    map[string]storage.TelemetryDiagnosticsCurrentRecord
	telemetrySecurityCurrent       map[string]storage.TelemetrySecurityInventoryCurrentRecord
	clients                        map[string]storage.ClientRecord
	clientAssignments              map[string]storage.ClientAssignmentRecord
	clientDeployments              map[string]storage.ClientDeploymentRecord
	jobs                           map[string]storage.JobRecord
	jobsByKey                      map[string]string
	jobTargets                     map[string]storage.JobTargetRecord
	auditEvents                    []storage.AuditEventRecord
	metricSnapshots                []storage.MetricSnapshotRecord
	enrollmentTokens               map[string]storage.EnrollmentTokenRecord
	agentCertificateRecoveryGrants map[string]storage.AgentCertificateRecoveryGrantRecord
	discoveredClients              map[string]storage.DiscoveredClientRecord
	sessions                       map[string]storage.SessionRecord
	loginLockouts                  map[string]storage.LoginLockoutRecord
	agentRevocations               map[string]storage.AgentRevocationRecord
	agentFallbackState             map[string]storage.AgentFallbackStateRecord
	panelSettings                  *storage.PanelSettingsRecord
	retentionSettings              *storage.RetentionSettings
	updateSettings                 json.RawMessage
	updateState                    json.RawMessage
	geoipSettings                  json.RawMessage
	geoipState                     json.RawMessage
	certificateAuthority           *storage.CertificateAuthorityRecord
	integrationProviders           map[string]storage.IntegrationProviderRecord
	fleetGroupIntegrations         map[string]storage.FleetGroupIntegrationRecord
	agentConfigTargets             map[string]storage.AgentConfigTargetRecord
	configApplyBatches             map[string]storage.ConfigApplyBatchRecord
	configApplyBatchTargets        map[string][]storage.ConfigApplyBatchTargetRecord
	cpSecrets                      map[string][]byte
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		users:                          make(map[string]storage.UserRecord),
		usernames:                      make(map[string]string),
		userAppearance:                 make(map[string]storage.UserAppearanceRecord),
		fleetGroups:                    make(map[string]storage.FleetGroupRecord),
		agents:                         make(map[string]storage.AgentRecord),
		instances:                      make(map[string]storage.InstanceRecord),
		telemetryRuntimeCurrent:        make(map[string]storage.TelemetryRuntimeCurrentRecord),
		telemetryRuntimeDCs:            make(map[string][]storage.TelemetryRuntimeDCRecord),
		telemetryRuntimeUpstreams:      make(map[string][]storage.TelemetryRuntimeUpstreamRecord),
		telemetryRuntimeEvents:         make(map[string][]storage.TelemetryRuntimeEventRecord),
		telemetryDiagnosticsCurrent:    make(map[string]storage.TelemetryDiagnosticsCurrentRecord),
		telemetrySecurityCurrent:       make(map[string]storage.TelemetrySecurityInventoryCurrentRecord),
		clients:                        make(map[string]storage.ClientRecord),
		clientAssignments:              make(map[string]storage.ClientAssignmentRecord),
		clientDeployments:              make(map[string]storage.ClientDeploymentRecord),
		jobs:                           make(map[string]storage.JobRecord),
		jobsByKey:                      make(map[string]string),
		jobTargets:                     make(map[string]storage.JobTargetRecord),
		auditEvents:                    make([]storage.AuditEventRecord, 0),
		metricSnapshots:                make([]storage.MetricSnapshotRecord, 0),
		enrollmentTokens:               make(map[string]storage.EnrollmentTokenRecord),
		agentCertificateRecoveryGrants: make(map[string]storage.AgentCertificateRecoveryGrantRecord),
		discoveredClients:              make(map[string]storage.DiscoveredClientRecord),
		sessions:                       make(map[string]storage.SessionRecord),
		loginLockouts:                  make(map[string]storage.LoginLockoutRecord),
		agentRevocations:               make(map[string]storage.AgentRevocationRecord),
		agentFallbackState:             make(map[string]storage.AgentFallbackStateRecord),
		integrationProviders:           make(map[string]storage.IntegrationProviderRecord),
		fleetGroupIntegrations:         make(map[string]storage.FleetGroupIntegrationRecord),
		agentConfigTargets:             make(map[string]storage.AgentConfigTargetRecord),
		configApplyBatches:             make(map[string]storage.ConfigApplyBatchRecord),
		configApplyBatchTargets:        make(map[string][]storage.ConfigApplyBatchTargetRecord),
		cpSecrets:                      make(map[string][]byte),
	}
}

func agentConfigTargetKey(scopeType, scopeID string) string {
	return scopeType + "\x00" + scopeID
}

func (s *memoryStore) GetAgentConfigTarget(_ context.Context, scopeType, scopeID string) (storage.AgentConfigTargetRecord, error) {
	rec, ok := s.agentConfigTargets[agentConfigTargetKey(scopeType, scopeID)]
	if !ok {
		return storage.AgentConfigTargetRecord{}, storage.ErrNotFound
	}
	return rec, nil
}

func (s *memoryStore) ListAgentConfigTargets(_ context.Context) ([]storage.AgentConfigTargetRecord, error) {
	out := make([]storage.AgentConfigTargetRecord, 0, len(s.agentConfigTargets))
	for _, rec := range s.agentConfigTargets {
		out = append(out, rec)
	}
	return out, nil
}

func (s *memoryStore) UpsertAgentConfigTarget(_ context.Context, rec storage.AgentConfigTargetRecord) error {
	s.agentConfigTargets[agentConfigTargetKey(rec.ScopeType, rec.ScopeID)] = rec
	return nil
}

func (s *memoryStore) DeleteAgentConfigTarget(_ context.Context, scopeType, scopeID string) (int64, error) {
	key := agentConfigTargetKey(scopeType, scopeID)
	if _, ok := s.agentConfigTargets[key]; !ok {
		return 0, nil
	}
	delete(s.agentConfigTargets, key)
	return 1, nil
}

func (s *memoryStore) CreateConfigApplyBatch(_ context.Context, b storage.ConfigApplyBatchRecord, targets []storage.ConfigApplyBatchTargetRecord) error {
	s.configApplyBatches[b.ID] = b
	stored := append([]storage.ConfigApplyBatchTargetRecord(nil), targets...)
	sortConfigApplyBatchTargets(stored)
	s.configApplyBatchTargets[b.ID] = stored
	return nil
}

func (s *memoryStore) GetConfigApplyBatch(_ context.Context, id string) (storage.ConfigApplyBatchRecord, []storage.ConfigApplyBatchTargetRecord, error) {
	b, ok := s.configApplyBatches[id]
	if !ok {
		return storage.ConfigApplyBatchRecord{}, nil, storage.ErrNotFound
	}
	targets := append([]storage.ConfigApplyBatchTargetRecord(nil), s.configApplyBatchTargets[id]...)
	return b, targets, nil
}

func (s *memoryStore) ListRunningConfigApplyBatches(_ context.Context) ([]storage.ConfigApplyBatchRecord, error) {
	out := make([]storage.ConfigApplyBatchRecord, 0)
	for _, b := range s.configApplyBatches {
		if b.Status == storage.ConfigApplyBatchStatusRunning {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *memoryStore) ActiveConfigApplyBatchForGroup(_ context.Context, fleetGroupID string) (storage.ConfigApplyBatchRecord, bool, error) {
	var found *storage.ConfigApplyBatchRecord
	for _, b := range s.configApplyBatches {
		if b.FleetGroupID != fleetGroupID || b.Status != storage.ConfigApplyBatchStatusRunning {
			continue
		}
		if found == nil || b.CreatedAt.Before(found.CreatedAt) || (b.CreatedAt.Equal(found.CreatedAt) && b.ID < found.ID) {
			bCopy := b
			found = &bCopy
		}
	}
	if found == nil {
		return storage.ConfigApplyBatchRecord{}, false, nil
	}
	return *found, true, nil
}

func (s *memoryStore) UpdateConfigApplyBatchStatus(_ context.Context, id, status string, now time.Time) error {
	b, ok := s.configApplyBatches[id]
	if !ok {
		return storage.ErrNotFound
	}
	b.Status = status
	b.UpdatedAt = now
	s.configApplyBatches[id] = b
	return nil
}

func (s *memoryStore) SetConfigApplyBatchTargetJob(_ context.Context, batchID, agentID, jobID, status string) error {
	targets := s.configApplyBatchTargets[batchID]
	for i := range targets {
		if targets[i].AgentID == agentID {
			targets[i].JobID = jobID
			targets[i].Status = status
			return nil
		}
	}
	return nil
}

func (s *memoryStore) UpdateConfigApplyBatchTargetStatus(_ context.Context, batchID, agentID, status, message string) error {
	targets := s.configApplyBatchTargets[batchID]
	for i := range targets {
		if targets[i].AgentID == agentID {
			targets[i].Status = status
			targets[i].Message = message
			return nil
		}
	}
	return nil
}

func (s *memoryStore) PruneConfigApplyBatches(_ context.Context, before time.Time) (int64, error) {
	var pruned int64
	for id, b := range s.configApplyBatches {
		terminal := b.Status == storage.ConfigApplyBatchStatusSucceeded ||
			b.Status == storage.ConfigApplyBatchStatusFailed ||
			b.Status == storage.ConfigApplyBatchStatusHalted
		if terminal && b.UpdatedAt.Before(before) {
			delete(s.configApplyBatches, id)
			delete(s.configApplyBatchTargets, id)
			pruned++
		}
	}
	return pruned, nil
}

// sortConfigApplyBatchTargets orders targets by wave_index then agent_id,
// matching the SQL backends' GetConfigApplyBatch ORDER BY clause.
func sortConfigApplyBatchTargets(targets []storage.ConfigApplyBatchTargetRecord) {
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].WaveIndex != targets[j].WaveIndex {
			return targets[i].WaveIndex < targets[j].WaveIndex
		}
		return targets[i].AgentID < targets[j].AgentID
	})
}

func (s *memoryStore) UpsertLoginLockout(_ context.Context, record storage.LoginLockoutRecord) error {
	s.loginLockouts[record.Username] = record
	return nil
}

func (s *memoryStore) GetLoginLockout(_ context.Context, username string) (storage.LoginLockoutRecord, error) {
	record, ok := s.loginLockouts[username]
	if !ok {
		return storage.LoginLockoutRecord{}, storage.ErrNotFound
	}
	return record, nil
}

func (s *memoryStore) DeleteLoginLockout(_ context.Context, username string) error {
	delete(s.loginLockouts, username)
	return nil
}

func (s *memoryStore) ListLoginLockouts(_ context.Context) ([]storage.LoginLockoutRecord, error) {
	out := make([]storage.LoginLockoutRecord, 0, len(s.loginLockouts))
	for _, record := range s.loginLockouts {
		out = append(out, record)
	}
	return out, nil
}

func (s *memoryStore) DeleteExpiredLoginLockouts(_ context.Context, before time.Time) (int64, error) {
	var deleted int64
	for username, record := range s.loginLockouts {
		if record.UpdatedAt.Before(before) {
			delete(s.loginLockouts, username)
			deleted++
		}
	}
	return deleted, nil
}

func (s *memoryStore) Ping(_ context.Context) error {
	return nil
}

func (s *memoryStore) Close() error {
	return nil
}

// R-S-14: stub fleet-scope methods so memoryStore continues to satisfy
// the Store interface. The contract tests do not exercise the scope
// surface directly — production behaviour is covered in the
// fleet_scope_test.go unit tests + future multi-tenant integration
// tests.
func (s *memoryStore) ListUserFleetGroupScopes(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (s *memoryStore) ListAllUserFleetGroupScopes(_ context.Context) ([]storage.UserFleetGroupScopeRecord, error) {
	return nil, nil
}

func (s *memoryStore) SetUserFleetGroupScopes(_ context.Context, _ string, _ []string, _ string, _ time.Time) error {
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
			UserID:   userID,
			Theme:    "system",
			Density:  "comfortable",
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

func (s *memoryStore) CreateFleetGroup(_ context.Context, group storage.FleetGroupRecord) error {
	if _, ok := s.fleetGroups[group.ID]; ok {
		return fmt.Errorf("fleet group %q already exists", group.ID)
	}
	s.fleetGroups[group.ID] = group
	return nil
}

func (s *memoryStore) UpdateFleetGroup(_ context.Context, group storage.FleetGroupRecord) error {
	existing, ok := s.fleetGroups[group.ID]
	if !ok {
		return storage.ErrNotFound
	}
	// Name is immutable — preserve it.
	existing.Label = group.Label
	existing.Description = group.Description
	existing.UpdatedAt = group.UpdatedAt
	s.fleetGroups[group.ID] = existing
	return nil
}

func (s *memoryStore) GetFleetGroup(_ context.Context, id string) (storage.FleetGroupRecord, error) {
	group, ok := s.fleetGroups[id]
	if !ok {
		return storage.FleetGroupRecord{}, storage.ErrNotFound
	}
	return group, nil
}

func (s *memoryStore) GetFleetGroupByName(_ context.Context, name string) (storage.FleetGroupRecord, error) {
	for _, group := range s.fleetGroups {
		if group.Name == name {
			return group, nil
		}
	}
	return storage.FleetGroupRecord{}, storage.ErrNotFound
}

func (s *memoryStore) DeleteFleetGroup(_ context.Context, id string) error {
	if _, ok := s.fleetGroups[id]; !ok {
		return storage.ErrNotFound
	}
	delete(s.fleetGroups, id)
	return nil
}

func (s *memoryStore) CountFleetGroupMembers(_ context.Context, fleetGroupID string) (storage.ReassignCounts, error) {
	var counts storage.ReassignCounts
	for _, agent := range s.agents {
		if agent.FleetGroupID == fleetGroupID {
			counts.Agents++
		}
	}
	for _, token := range s.enrollmentTokens {
		if token.FleetGroupID == fleetGroupID {
			counts.EnrollmentTokens++
		}
	}
	for _, assignment := range s.clientAssignments {
		if assignment.FleetGroupID == fleetGroupID {
			counts.ClientAssignments++
		}
	}
	return counts, nil
}

func (s *memoryStore) ReassignFleetGroupMembers(_ context.Context, fromID, toID string) (storage.ReassignCounts, error) {
	var counts storage.ReassignCounts
	for id, agent := range s.agents {
		if agent.FleetGroupID == fromID {
			agent.FleetGroupID = toID
			s.agents[id] = agent
			counts.Agents++
		}
	}
	for id, token := range s.enrollmentTokens {
		if token.FleetGroupID == fromID {
			token.FleetGroupID = toID
			s.enrollmentTokens[id] = token
			counts.EnrollmentTokens++
		}
	}
	for id, assignment := range s.clientAssignments {
		if assignment.FleetGroupID == fromID {
			assignment.FleetGroupID = toID
			s.clientAssignments[id] = assignment
			counts.ClientAssignments++
		}
	}
	return counts, nil
}

func (s *memoryStore) CreateIntegrationProvider(_ context.Context, provider storage.IntegrationProviderRecord) error {
	if _, ok := s.integrationProviders[provider.ID]; ok {
		return fmt.Errorf("integration provider %q already exists", provider.ID)
	}
	s.integrationProviders[provider.ID] = provider
	return nil
}

func (s *memoryStore) UpdateIntegrationProvider(_ context.Context, provider storage.IntegrationProviderRecord) error {
	existing, ok := s.integrationProviders[provider.ID]
	if !ok {
		return storage.ErrNotFound
	}
	existing.Label = provider.Label
	existing.Config = provider.Config
	existing.UpdatedAt = provider.UpdatedAt
	s.integrationProviders[provider.ID] = existing
	return nil
}

func (s *memoryStore) GetIntegrationProvider(_ context.Context, id string) (storage.IntegrationProviderRecord, error) {
	p, ok := s.integrationProviders[id]
	if !ok {
		return storage.IntegrationProviderRecord{}, storage.ErrNotFound
	}
	return p, nil
}

func (s *memoryStore) ListIntegrationProviders(_ context.Context) ([]storage.IntegrationProviderRecord, error) {
	result := make([]storage.IntegrationProviderRecord, 0, len(s.integrationProviders))
	for _, p := range s.integrationProviders {
		result = append(result, p)
	}
	return result, nil
}

func (s *memoryStore) ListIntegrationProvidersByKind(_ context.Context, kind string) ([]storage.IntegrationProviderRecord, error) {
	result := make([]storage.IntegrationProviderRecord, 0)
	for _, p := range s.integrationProviders {
		if p.Kind == kind {
			result = append(result, p)
		}
	}
	return result, nil
}

func (s *memoryStore) DeleteIntegrationProvider(_ context.Context, id string) error {
	if _, ok := s.integrationProviders[id]; !ok {
		return storage.ErrNotFound
	}
	delete(s.integrationProviders, id)
	return nil
}

func (s *memoryStore) CreateFleetGroupIntegration(_ context.Context, integration storage.FleetGroupIntegrationRecord) error {
	for _, existing := range s.fleetGroupIntegrations {
		if existing.FleetGroupID == integration.FleetGroupID && existing.Kind == integration.Kind {
			return fmt.Errorf("integration %q already installed on fleet group %q", integration.Kind, integration.FleetGroupID)
		}
	}
	s.fleetGroupIntegrations[integration.ID] = integration
	return nil
}

func (s *memoryStore) UpdateFleetGroupIntegration(_ context.Context, integration storage.FleetGroupIntegrationRecord) error {
	existing, ok := s.fleetGroupIntegrations[integration.ID]
	if !ok {
		return storage.ErrNotFound
	}
	existing.ProviderID = integration.ProviderID
	existing.Config = integration.Config
	existing.Enabled = integration.Enabled
	existing.UpdatedAt = integration.UpdatedAt
	s.fleetGroupIntegrations[integration.ID] = existing
	return nil
}

func (s *memoryStore) GetFleetGroupIntegration(_ context.Context, id string) (storage.FleetGroupIntegrationRecord, error) {
	i, ok := s.fleetGroupIntegrations[id]
	if !ok {
		return storage.FleetGroupIntegrationRecord{}, storage.ErrNotFound
	}
	return i, nil
}

func (s *memoryStore) ListFleetGroupIntegrations(_ context.Context, fleetGroupID string) ([]storage.FleetGroupIntegrationRecord, error) {
	result := make([]storage.FleetGroupIntegrationRecord, 0)
	for _, i := range s.fleetGroupIntegrations {
		if i.FleetGroupID == fleetGroupID {
			result = append(result, i)
		}
	}
	return result, nil
}

func (s *memoryStore) DeleteFleetGroupIntegration(_ context.Context, id string) error {
	if _, ok := s.fleetGroupIntegrations[id]; !ok {
		return storage.ErrNotFound
	}
	delete(s.fleetGroupIntegrations, id)
	return nil
}

func (s *memoryStore) PutAgent(_ context.Context, agent storage.AgentRecord) error {
	s.agents[agent.ID] = agent
	return nil
}

func (s *memoryStore) PutAgentsBulk(ctx context.Context, agents []storage.AgentRecord) error {
	for _, agent := range agents {
		if err := s.PutAgent(ctx, agent); err != nil {
			return err
		}
	}
	return nil
}

func (s *memoryStore) ListAgents(_ context.Context) ([]storage.AgentRecord, error) {
	result := make([]storage.AgentRecord, 0, len(s.agents))
	for _, agent := range s.agents {
		result = append(result, agent)
	}

	return result, nil
}

func (s *memoryStore) EarliestAgentCertExpiry(_ context.Context) (*time.Time, error) {
	var earliest *time.Time
	for _, agent := range s.agents {
		if agent.CertExpiresAt == nil {
			continue
		}
		if earliest == nil || agent.CertExpiresAt.Before(*earliest) {
			earliest = agent.CertExpiresAt
		}
	}
	return earliest, nil
}

func (s *memoryStore) DeleteAgent(_ context.Context, agentID string) error {
	if _, ok := s.agents[agentID]; !ok {
		return storage.ErrNotFound
	}
	delete(s.agents, agentID)
	// Mirror the FK ON DELETE CASCADE that the real backends apply
	// (sqlite migration 0028, postgres migrations 0022 + 0028, sqlite
	// migration 0031 for agent_fallback_state) so the memory backend
	// stays contract-compatible.
	delete(s.agentCertificateRecoveryGrants, agentID)
	delete(s.agentFallbackState, agentID)
	for id, dc := range s.discoveredClients {
		if dc.AgentID == agentID {
			delete(s.discoveredClients, id)
		}
	}
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

func (s *memoryStore) UpdateAgentFleetGroup(_ context.Context, agentID, fleetGroupID string) error {
	agent, ok := s.agents[agentID]
	if !ok {
		return storage.ErrNotFound
	}
	agent.FleetGroupID = fleetGroupID
	s.agents[agentID] = agent
	return nil
}

func (s *memoryStore) UpdateAgentCertSerial(_ context.Context, agentID string, serial string) error {
	agent, ok := s.agents[agentID]
	if !ok {
		return storage.ErrNotFound
	}
	agent.CertSerial = serial
	s.agents[agentID] = agent
	return nil
}

func (s *memoryStore) GetAgentCertSerial(_ context.Context, agentID string) (string, error) {
	agent, ok := s.agents[agentID]
	if !ok {
		return "", storage.ErrNotFound
	}
	return agent.CertSerial, nil
}

func (s *memoryStore) UpdateAgentTransportMode(_ context.Context, agentID, _, _ string) error {
	if _, ok := s.agents[agentID]; !ok {
		return storage.ErrNotFound
	}
	return nil
}

func (s *memoryStore) UpdateAgentCertPin(_ context.Context, agentID string, pin []byte) error {
	agent, ok := s.agents[agentID]
	if !ok {
		return storage.ErrNotFound
	}
	agent.CertSPKISHA256 = pin
	s.agents[agentID] = agent
	return nil
}

func (s *memoryStore) GetAgentCertPin(_ context.Context, agentID string) ([]byte, error) {
	agent, ok := s.agents[agentID]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return agent.CertSPKISHA256, nil
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

func (s *memoryStore) PutInstancesBulk(ctx context.Context, instances []storage.InstanceRecord) error {
	for _, instance := range instances {
		if err := s.PutInstance(ctx, instance); err != nil {
			return err
		}
	}
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

func (s *memoryStore) ListAllTelemetryRuntimeDCs(_ context.Context) ([]storage.TelemetryRuntimeDCRecord, error) {
	result := make([]storage.TelemetryRuntimeDCRecord, 0)
	for _, records := range s.telemetryRuntimeDCs {
		result = append(result, records...)
	}

	return result, nil
}

func (s *memoryStore) ReplaceTelemetryRuntimeUpstreams(_ context.Context, agentID string, records []storage.TelemetryRuntimeUpstreamRecord) error {
	s.telemetryRuntimeUpstreams[agentID] = append([]storage.TelemetryRuntimeUpstreamRecord(nil), records...)
	return nil
}

func (s *memoryStore) ListTelemetryRuntimeUpstreams(_ context.Context, agentID string) ([]storage.TelemetryRuntimeUpstreamRecord, error) {
	return append([]storage.TelemetryRuntimeUpstreamRecord(nil), s.telemetryRuntimeUpstreams[agentID]...), nil
}

func (s *memoryStore) ListAllTelemetryRuntimeUpstreams(_ context.Context) ([]storage.TelemetryRuntimeUpstreamRecord, error) {
	result := make([]storage.TelemetryRuntimeUpstreamRecord, 0)
	for _, records := range s.telemetryRuntimeUpstreams {
		result = append(result, records...)
	}

	return result, nil
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

func (s *memoryStore) ListAllTelemetryRuntimeEventsPerAgent(_ context.Context, perAgentLimit int) ([]storage.TelemetryRuntimeEventRecord, error) {
	result := make([]storage.TelemetryRuntimeEventRecord, 0)
	for _, events := range s.telemetryRuntimeEvents {
		records := append([]storage.TelemetryRuntimeEventRecord(nil), events...)
		// Events are appended in ascending order, so the newest
		// perAgentLimit are the tail — matching ListTelemetryRuntimeEvents.
		if perAgentLimit > 0 && len(records) > perAgentLimit {
			records = records[len(records)-perAgentLimit:]
		}
		result = append(result, records...)
	}

	return result, nil
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

func (s *memoryStore) PutTelemetryRuntimeCurrentBulk(_ context.Context, records []storage.TelemetryRuntimeCurrentRecord) error {
	for _, r := range records {
		s.telemetryRuntimeCurrent[r.AgentID] = r
	}
	return nil
}

func (s *memoryStore) ReplaceTelemetryRuntimeDCsBulk(_ context.Context, byAgent map[string][]storage.TelemetryRuntimeDCRecord) error {
	for agentID, records := range byAgent {
		s.telemetryRuntimeDCs[agentID] = append([]storage.TelemetryRuntimeDCRecord(nil), records...)
	}
	return nil
}

func (s *memoryStore) ReplaceTelemetryRuntimeUpstreamsBulk(_ context.Context, byAgent map[string][]storage.TelemetryRuntimeUpstreamRecord) error {
	for agentID, records := range byAgent {
		s.telemetryRuntimeUpstreams[agentID] = append([]storage.TelemetryRuntimeUpstreamRecord(nil), records...)
	}
	return nil
}

func (s *memoryStore) AppendTelemetryRuntimeEventsBulk(_ context.Context, records []storage.TelemetryRuntimeEventRecord) error {
	// Upsert by (agent_id, sequence) so a duplicate sequence within one
	// batch collapses to last-wins, matching the SQL backends' conflict
	// target on (agent_id, sequence).
	for _, r := range records {
		events := s.telemetryRuntimeEvents[r.AgentID]
		replaced := false
		for i := range events {
			if events[i].Sequence == r.Sequence {
				events[i] = r
				replaced = true
				break
			}
		}
		if !replaced {
			events = append(events, r)
		}
		s.telemetryRuntimeEvents[r.AgentID] = events
	}
	return nil
}

func (s *memoryStore) PutTelemetryDiagnosticsCurrentBulk(_ context.Context, records []storage.TelemetryDiagnosticsCurrentRecord) error {
	for _, r := range records {
		s.telemetryDiagnosticsCurrent[r.AgentID] = r
	}
	return nil
}

func (s *memoryStore) PutTelemetrySecurityInventoryCurrentBulk(_ context.Context, records []storage.TelemetrySecurityInventoryCurrentRecord) error {
	for _, r := range records {
		s.telemetrySecurityCurrent[r.AgentID] = r
	}
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

func (s *memoryStore) GetJob(_ context.Context, id string) (storage.JobRecord, error) {
	job, ok := s.jobs[id]
	if !ok {
		return storage.JobRecord{}, storage.ErrNotFound
	}
	return job, nil
}

func (s *memoryStore) ListJobs(_ context.Context) ([]storage.JobRecord, error) {
	result := make([]storage.JobRecord, 0, len(s.jobs))
	for _, job := range s.jobs {
		result = append(result, job)
	}

	return result, nil
}

// ListJobsCursor mirrors the SQL contract: (created_at DESC, id DESC) order,
// limit+1 fetch to detect "more". The memory store keeps jobs in a map so
// we materialise + sort each call; that's fine for tests since the volume
// is tiny.
func (s *memoryStore) ListJobsCursor(_ context.Context, params storage.ListJobsCursorParams) ([]storage.JobRecord, storage.ListJobsCursorParams, error) {
	limit := storage.NormalizeCursorLimit(params.Limit)
	all := make([]storage.JobRecord, 0, len(s.jobs))
	for _, job := range s.jobs {
		all = append(all, job)
	}
	// Sort newest-first so iteration mirrors the SQL ORDER BY.
	sortJobsDesc(all)

	out := make([]storage.JobRecord, 0, limit+1)
	for _, job := range all {
		if !params.AfterCreatedAt.IsZero() || params.AfterID != "" {
			if !jobAfterCursor(job, params.AfterCreatedAt, params.AfterID) {
				continue
			}
		}
		out = append(out, job)
		if len(out) > limit {
			break
		}
	}
	var next storage.ListJobsCursorParams
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = storage.ListJobsCursorParams{
			Limit:          limit,
			AfterCreatedAt: last.CreatedAt,
			AfterID:        last.ID,
		}
	}
	return out, next, nil
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

func (s *memoryStore) ListAllJobTargets(_ context.Context) ([]storage.JobTargetRecord, error) {
	result := make([]storage.JobTargetRecord, 0, len(s.jobTargets))
	for _, target := range s.jobTargets {
		result = append(result, target)
	}
	return result, nil
}

func (s *memoryStore) AppendAuditEvent(_ context.Context, event storage.AuditEventRecord) error {
	s.auditEvents = append(s.auditEvents, event)
	return nil
}

func (s *memoryStore) AppendAuditEventsBulk(_ context.Context, events []storage.AuditEventRecord) error {
	s.auditEvents = append(s.auditEvents, events...)
	return nil
}

func (s *memoryStore) LatestAuditChainHash(_ context.Context) (string, error) {
	if len(s.auditEvents) == 0 {
		return "", nil
	}
	// Mirrors the postgres/sqlite contract: most recently inserted
	// row wins. The in-memory store appends in arrival order so the
	// last entry is the youngest.
	return s.auditEvents[len(s.auditEvents)-1].EventHash, nil
}

func (s *memoryStore) ListAuditEvents(_ context.Context, limit int) ([]storage.AuditEventRecord, error) {
	events := append([]storage.AuditEventRecord(nil), s.auditEvents...)
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}

func (s *memoryStore) ListAuditEventsCursor(_ context.Context, params storage.ListAuditEventsCursorParams) ([]storage.AuditEventRecord, storage.ListAuditEventsCursorParams, error) {
	limit := storage.NormalizeCursorLimit(params.Limit)
	all := append([]storage.AuditEventRecord(nil), s.auditEvents...)
	sortAuditDesc(all)

	out := make([]storage.AuditEventRecord, 0, limit+1)
	for _, e := range all {
		if !params.AfterCreatedAt.IsZero() || params.AfterID != "" {
			if !auditAfterCursor(e, params.AfterCreatedAt, params.AfterID) {
				continue
			}
		}
		out = append(out, e)
		if len(out) > limit {
			break
		}
	}
	var next storage.ListAuditEventsCursorParams
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = storage.ListAuditEventsCursorParams{
			Limit:          limit,
			AfterCreatedAt: last.CreatedAt,
			AfterID:        last.ID,
		}
	}
	return out, next, nil
}

func (s *memoryStore) PruneAuditEvents(_ context.Context, before time.Time) (int64, error) {
	var pruned int64
	kept := s.auditEvents[:0]
	for _, e := range s.auditEvents {
		if e.CreatedAt.Before(before) {
			pruned++
			continue
		}
		kept = append(kept, e)
	}
	s.auditEvents = kept
	return pruned, nil
}

func (s *memoryStore) AppendMetricSnapshot(_ context.Context, snapshot storage.MetricSnapshotRecord) error {
	s.metricSnapshots = append(s.metricSnapshots, snapshot)
	return nil
}

func (s *memoryStore) AppendMetricSnapshotsBulk(ctx context.Context, snapshots []storage.MetricSnapshotRecord) error {
	for _, snapshot := range snapshots {
		if err := s.AppendMetricSnapshot(ctx, snapshot); err != nil {
			return err
		}
	}
	return nil
}

// metricSnapshotCap mirrors the 512-row cap enforced by the SQLite and
// Postgres ListMetricSnapshots queries (M4) so the shared contract test
// (store_contract_metrics.go) exercises identical semantics on every
// backend, including this in-memory fixture.
const metricSnapshotCap = 512

func (s *memoryStore) ListMetricSnapshots(_ context.Context) ([]storage.MetricSnapshotRecord, error) {
	all := append([]storage.MetricSnapshotRecord(nil), s.metricSnapshots...)
	sort.Slice(all, func(i, j int) bool {
		if !all[i].CapturedAt.Equal(all[j].CapturedAt) {
			return all[i].CapturedAt.Before(all[j].CapturedAt)
		}
		return all[i].ID < all[j].ID
	})
	if len(all) > metricSnapshotCap {
		all = all[len(all)-metricSnapshotCap:]
	}
	return all, nil
}

func (s *memoryStore) PruneMetricSnapshots(_ context.Context, before time.Time) (int64, error) {
	var pruned int64
	kept := s.metricSnapshots[:0]
	for _, m := range s.metricSnapshots {
		if m.CapturedAt.Before(before) {
			pruned++
			continue
		}
		kept = append(kept, m)
	}
	s.metricSnapshots = kept
	return pruned, nil
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

func (s *memoryStore) PutRetentionSettings(_ context.Context, settings storage.RetentionSettings) error {
	copySettings := settings
	s.retentionSettings = &copySettings
	return nil
}

func (s *memoryStore) GetRetentionSettings(_ context.Context) (storage.RetentionSettings, error) {
	if s.retentionSettings == nil {
		return storage.RetentionSettings{}, storage.ErrNotFound
	}
	return *s.retentionSettings, nil
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

func (s *memoryStore) PutGeoIPSettings(_ context.Context, data json.RawMessage) error {
	s.geoipSettings = append(json.RawMessage(nil), data...)
	return nil
}

func (s *memoryStore) GetGeoIPSettings(_ context.Context) (json.RawMessage, error) {
	if s.geoipSettings == nil {
		return nil, nil
	}
	return append(json.RawMessage(nil), s.geoipSettings...), nil
}

func (s *memoryStore) PutGeoIPState(_ context.Context, data json.RawMessage) error {
	s.geoipState = append(json.RawMessage(nil), data...)
	return nil
}

func (s *memoryStore) GetGeoIPState(_ context.Context) (json.RawMessage, error) {
	if s.geoipState == nil {
		return nil, nil
	}
	return append(json.RawMessage(nil), s.geoipState...), nil
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

func (s *memoryStore) PruneEnrollmentTokens(_ context.Context, before time.Time) (int64, error) {
	var pruned int64
	for value, rec := range s.enrollmentTokens {
		dead := (rec.ConsumedAt != nil && rec.ConsumedAt.Before(before)) ||
			(rec.RevokedAt != nil && rec.RevokedAt.Before(before)) ||
			(rec.ConsumedAt == nil && rec.RevokedAt == nil && rec.ExpiresAt.Before(before))
		if dead {
			delete(s.enrollmentTokens, value)
			pruned++
		}
	}
	return pruned, nil
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

func (s *memoryStore) PutAgentFallbackState(_ context.Context, rec storage.AgentFallbackStateRecord) error {
	// Mirror INSERT ... ON CONFLICT DO NOTHING: first writer wins.
	if _, ok := s.agentFallbackState[rec.AgentID]; ok {
		return nil
	}
	s.agentFallbackState[rec.AgentID] = rec
	return nil
}

func (s *memoryStore) DeleteAgentFallbackState(_ context.Context, agentID string) error {
	delete(s.agentFallbackState, agentID)
	return nil
}

func (s *memoryStore) GetAgentFallbackState(_ context.Context, agentID string) (storage.AgentFallbackStateRecord, error) {
	rec, ok := s.agentFallbackState[agentID]
	if !ok {
		return storage.AgentFallbackStateRecord{}, storage.ErrNotFound
	}
	return rec, nil
}

func (s *memoryStore) ListAgentFallbackState(_ context.Context) ([]storage.AgentFallbackStateRecord, error) {
	out := make([]storage.AgentFallbackStateRecord, 0, len(s.agentFallbackState))
	for _, rec := range s.agentFallbackState {
		out = append(out, rec)
	}
	return out, nil
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

func (s *memoryStore) GetDiscoveredClientByAgentAndName(_ context.Context, agentID string, clientName string) (storage.DiscoveredClientRecord, error) {
	for _, r := range s.discoveredClients {
		if r.AgentID == agentID && r.ClientName == clientName {
			return r, nil
		}
	}
	return storage.DiscoveredClientRecord{}, storage.ErrNotFound
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

func (s *memoryStore) UpdateDiscoveredClientStatusBulk(_ context.Context, ids []string, status string, updatedAt time.Time) error {
	for _, id := range ids {
		if r, ok := s.discoveredClients[id]; ok {
			r.Status = status
			r.UpdatedAt = updatedAt
			s.discoveredClients[id] = r
		}
	}
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

func (s *memoryStore) AppendServerLoadPointsBulk(_ context.Context, _ []storage.ServerLoadPointRecord) error {
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

func (s *memoryStore) AppendDCHealthPointsBulk(_ context.Context, _ []storage.DCHealthPointRecord) error {
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

func (s *memoryStore) UpsertClientIPHistoryBulk(_ context.Context, _ []storage.ClientIPHistoryRecord) error {
	return nil
}

func (s *memoryStore) ListClientIPHistory(_ context.Context, _ string, _ time.Time, _ time.Time) ([]storage.ClientIPHistoryRecord, error) {
	return nil, nil
}

func (s *memoryStore) AggregateClientIPHistory(_ context.Context, _ string, _ time.Time, _ time.Time, _ int) ([]storage.ClientIPAggregateRecord, error) {
	return nil, nil
}

func (s *memoryStore) UpsertClientUsage(_ context.Context, _ storage.ClientUsageRecord) error {
	return nil
}

func (s *memoryStore) ListClientUsage(_ context.Context) ([]storage.ClientUsageRecord, error) {
	return nil, nil
}

func (s *memoryStore) DeleteClientUsageByClient(_ context.Context, _ string) error {
	return nil
}

func (s *memoryStore) CountUniqueClientIPs(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (s *memoryStore) CountUniqueClientIPsForClients(_ context.Context, _ []string) (map[string]int, error) {
	return map[string]int{}, nil
}

func (s *memoryStore) ListServerLoadPointsForAgents(_ context.Context, _ []string, _, _ time.Time) (map[string][]storage.ServerLoadPointRecord, error) {
	return map[string][]storage.ServerLoadPointRecord{}, nil
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

func (s *memoryStore) TouchSession(_ context.Context, sessionID string, lastSeenAt time.Time) error {
	rec, ok := s.sessions[sessionID]
	if !ok {
		return storage.ErrNotFound
	}
	rec.LastSeenAt = lastSeenAt
	s.sessions[sessionID] = rec
	return nil
}

func (s *memoryStore) PruneTerminalJobs(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (s *memoryStore) GetCPSecret(_ context.Context, key string) ([]byte, error) {
	val, ok := s.cpSecrets[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return append([]byte(nil), val...), nil
}

func (s *memoryStore) PutCPSecret(_ context.Context, key string, value []byte) error {
	s.cpSecrets[key] = append([]byte(nil), value...)
	return nil
}

func (s *memoryStore) ListCPSecrets(_ context.Context) ([]storage.CPSecretRecord, error) {
	if len(s.cpSecrets) == 0 {
		return nil, nil
	}
	out := make([]storage.CPSecretRecord, 0, len(s.cpSecrets))
	for k, v := range s.cpSecrets {
		out = append(out, storage.CPSecretRecord{Key: k, Value: append([]byte(nil), v...)})
	}
	return out, nil
}

func (s *memoryStore) UpsertConsumedTotp(_ context.Context, _ storage.ConsumedTotpRecord) error {
	return nil
}

func (s *memoryStore) ListConsumedTotp(_ context.Context) ([]storage.ConsumedTotpRecord, error) {
	return nil, nil
}

func (s *memoryStore) DeleteExpiredConsumedTotp(_ context.Context, _ time.Time) error {
	return nil
}

// Transact on the memoryStore implements the contract via snapshot +
// restore under txMu. Serialization through txMu gives the "concurrent
// writers" contract test the atomicity it expects; deep-copy snapshot +
// restore gives it the rollback-on-error semantics. The tx handed to
// fn is a memoryTxStore whose Transact always returns
// ErrNestedTransact, enforcing the no-reentrancy contract without
// deadlocking on txMu.
func (s *memoryStore) Transact(ctx context.Context, fn storage.TxFn) (retErr error) {
	if fn == nil {
		return fmt.Errorf("memoryStore: Transact requires a non-nil TxFn")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.txMu.Lock()
	defer s.txMu.Unlock()

	snap := s.snapshot()
	defer func() {
		if p := recover(); p != nil {
			s.restore(snap)
			panic(p)
		}
		if retErr != nil {
			s.restore(snap)
		}
	}()

	return fn(&memoryTxStore{memoryStore: s})
}

// memoryTxStore wraps memoryStore during a Transact callback. All
// Store methods delegate to the underlying memoryStore directly (the
// callback already runs under txMu), but Transact returns
// ErrNestedTransact so nested calls are rejected without deadlock.
type memoryTxStore struct {
	*memoryStore
}

func (t *memoryTxStore) Transact(_ context.Context, _ storage.TxFn) error {
	return storage.ErrNestedTransact
}

// memoryStoreSnapshot holds a deep copy of every mutable field on
// memoryStore so Transact can roll back on error.
type memoryStoreSnapshot struct {
	users                          map[string]storage.UserRecord
	usernames                      map[string]string
	userAppearance                 map[string]storage.UserAppearanceRecord
	fleetGroups                    map[string]storage.FleetGroupRecord
	agents                         map[string]storage.AgentRecord
	instances                      map[string]storage.InstanceRecord
	telemetryRuntimeCurrent        map[string]storage.TelemetryRuntimeCurrentRecord
	telemetryRuntimeDCs            map[string][]storage.TelemetryRuntimeDCRecord
	telemetryRuntimeUpstreams      map[string][]storage.TelemetryRuntimeUpstreamRecord
	telemetryRuntimeEvents         map[string][]storage.TelemetryRuntimeEventRecord
	telemetryDiagnosticsCurrent    map[string]storage.TelemetryDiagnosticsCurrentRecord
	telemetrySecurityCurrent       map[string]storage.TelemetrySecurityInventoryCurrentRecord
	clients                        map[string]storage.ClientRecord
	clientAssignments              map[string]storage.ClientAssignmentRecord
	clientDeployments              map[string]storage.ClientDeploymentRecord
	jobs                           map[string]storage.JobRecord
	jobsByKey                      map[string]string
	jobTargets                     map[string]storage.JobTargetRecord
	auditEvents                    []storage.AuditEventRecord
	metricSnapshots                []storage.MetricSnapshotRecord
	enrollmentTokens               map[string]storage.EnrollmentTokenRecord
	agentCertificateRecoveryGrants map[string]storage.AgentCertificateRecoveryGrantRecord
	discoveredClients              map[string]storage.DiscoveredClientRecord
	sessions                       map[string]storage.SessionRecord
	agentRevocations               map[string]storage.AgentRevocationRecord
	agentFallbackState             map[string]storage.AgentFallbackStateRecord
	panelSettings                  *storage.PanelSettingsRecord
	retentionSettings              *storage.RetentionSettings
	updateSettings                 json.RawMessage
	updateState                    json.RawMessage
	geoipSettings                  json.RawMessage
	geoipState                     json.RawMessage
	certificateAuthority           *storage.CertificateAuthorityRecord
	cpSecrets                      map[string][]byte
}

func copyMap[K comparable, V any](in map[K]V) map[K]V {
	if in == nil {
		return nil
	}
	out := make(map[K]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copySliceMap[K comparable, V any](in map[K][]V) map[K][]V {
	if in == nil {
		return nil
	}
	out := make(map[K][]V, len(in))
	for k, v := range in {
		out[k] = append([]V(nil), v...)
	}
	return out
}

func (s *memoryStore) snapshot() memoryStoreSnapshot {
	snap := memoryStoreSnapshot{
		users:                          copyMap(s.users),
		usernames:                      copyMap(s.usernames),
		userAppearance:                 copyMap(s.userAppearance),
		fleetGroups:                    copyMap(s.fleetGroups),
		agents:                         copyMap(s.agents),
		instances:                      copyMap(s.instances),
		telemetryRuntimeCurrent:        copyMap(s.telemetryRuntimeCurrent),
		telemetryRuntimeDCs:            copySliceMap(s.telemetryRuntimeDCs),
		telemetryRuntimeUpstreams:      copySliceMap(s.telemetryRuntimeUpstreams),
		telemetryRuntimeEvents:         copySliceMap(s.telemetryRuntimeEvents),
		telemetryDiagnosticsCurrent:    copyMap(s.telemetryDiagnosticsCurrent),
		telemetrySecurityCurrent:       copyMap(s.telemetrySecurityCurrent),
		clients:                        copyMap(s.clients),
		clientAssignments:              copyMap(s.clientAssignments),
		clientDeployments:              copyMap(s.clientDeployments),
		jobs:                           copyMap(s.jobs),
		jobsByKey:                      copyMap(s.jobsByKey),
		jobTargets:                     copyMap(s.jobTargets),
		auditEvents:                    append([]storage.AuditEventRecord(nil), s.auditEvents...),
		metricSnapshots:                append([]storage.MetricSnapshotRecord(nil), s.metricSnapshots...),
		enrollmentTokens:               copyMap(s.enrollmentTokens),
		agentCertificateRecoveryGrants: copyMap(s.agentCertificateRecoveryGrants),
		discoveredClients:              copyMap(s.discoveredClients),
		sessions:                       copyMap(s.sessions),
		agentRevocations:               copyMap(s.agentRevocations),
		agentFallbackState:             copyMap(s.agentFallbackState),
		updateSettings:                 append(json.RawMessage(nil), s.updateSettings...),
		updateState:                    append(json.RawMessage(nil), s.updateState...),
		geoipSettings:                  append(json.RawMessage(nil), s.geoipSettings...),
		geoipState:                     append(json.RawMessage(nil), s.geoipState...),
		cpSecrets:                      copySliceMap(s.cpSecrets),
	}
	if s.panelSettings != nil {
		ps := *s.panelSettings
		snap.panelSettings = &ps
	}
	if s.retentionSettings != nil {
		rs := *s.retentionSettings
		snap.retentionSettings = &rs
	}
	if s.certificateAuthority != nil {
		ca := *s.certificateAuthority
		snap.certificateAuthority = &ca
	}
	return snap
}

func (s *memoryStore) restore(snap memoryStoreSnapshot) {
	s.users = snap.users
	s.usernames = snap.usernames
	s.userAppearance = snap.userAppearance
	s.fleetGroups = snap.fleetGroups
	s.agents = snap.agents
	s.instances = snap.instances
	s.telemetryRuntimeCurrent = snap.telemetryRuntimeCurrent
	s.telemetryRuntimeDCs = snap.telemetryRuntimeDCs
	s.telemetryRuntimeUpstreams = snap.telemetryRuntimeUpstreams
	s.telemetryRuntimeEvents = snap.telemetryRuntimeEvents
	s.telemetryDiagnosticsCurrent = snap.telemetryDiagnosticsCurrent
	s.telemetrySecurityCurrent = snap.telemetrySecurityCurrent
	s.clients = snap.clients
	s.clientAssignments = snap.clientAssignments
	s.clientDeployments = snap.clientDeployments
	s.jobs = snap.jobs
	s.jobsByKey = snap.jobsByKey
	s.jobTargets = snap.jobTargets
	s.auditEvents = snap.auditEvents
	s.metricSnapshots = snap.metricSnapshots
	s.enrollmentTokens = snap.enrollmentTokens
	s.agentCertificateRecoveryGrants = snap.agentCertificateRecoveryGrants
	s.discoveredClients = snap.discoveredClients
	s.sessions = snap.sessions
	s.agentRevocations = snap.agentRevocations
	s.agentFallbackState = snap.agentFallbackState
	s.panelSettings = snap.panelSettings
	s.retentionSettings = snap.retentionSettings
	s.updateSettings = snap.updateSettings
	s.updateState = snap.updateState
	s.geoipSettings = snap.geoipSettings
	s.geoipState = snap.geoipState
	s.certificateAuthority = snap.certificateAuthority
	s.cpSecrets = snap.cpSecrets
}
