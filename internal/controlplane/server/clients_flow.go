package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Lock ordering invariant for the Server struct (P2-LOG-11 / M-C11 / L-08):
//
//	s.mu  ->  s.clientsMu  ->  s.metricsAuditMu
//
// Whenever two of these locks must be observed together, they MUST be taken
// in the order above and released in the reverse order. Reverse-order
// acquisition (e.g. clientsMu -> mu) deadlocks against applyAgentSnapshot,
// which holds s.mu while briefly taking s.clientsMu for client-usage writes.
//
// Functions that need data from BOTH sides (agents and clientAssignments)
// snapshot the needed fields under the first lock, release it, then take
// the second lock with a plain local copy — they never nest. See
// resolveClientTargetAgentIDs and resolveClientIDByName below for the
// snapshot pattern.

var (
	errClientNameRequired   = errors.New("client name is required")
	errClientUserADTag      = errors.New("user_ad_tag must contain exactly 32 hex characters")
	errClientExpiration     = errors.New("expiration_rfc3339 must be a valid RFC3339 timestamp")
	errClientTargetsRequired = errors.New("client must target at least one agent")
)

const clientJobTTL = 10 * time.Minute

type clientMutationInput struct {
	Name              string
	Secret            string
	Enabled           *bool
	UserADTag         string
	MaxTCPConns       int
	MaxUniqueIPs      int
	DataQuotaBytes    int64
	ExpirationRFC3339 string
	FleetGroupIDs     []string
	AgentIDs          []string
}

type clientJobPayload struct {
	ClientID           string `json:"client_id"`
	PreviousName       string `json:"previous_name,omitempty"`
	Name               string `json:"name"`
	Secret             string `json:"secret"`
	UserADTag          string `json:"user_ad_tag"`
	Enabled            bool   `json:"enabled"`
	MaxTCPConns        int    `json:"max_tcp_conns"`
	MaxUniqueIPs       int    `json:"max_unique_ips"`
	DataQuotaBytes     int64  `json:"data_quota_bytes"`
	ExpirationRFC3339  string `json:"expiration_rfc3339"`
}

type clientJobResultPayload struct {
	ConnectionLink string `json:"connection_link"`
}

// aggregatedClientUsage now lives in controlplane/clients as
// AggregatedUsage. Kept as a server-local alias so existing call sites
// (HTTP response composition, test assertions) keep compiling until
// they are renamed to use the clients package directly.
type aggregatedClientUsage = clients.AggregatedUsage

func (s *Server) restoreStoredClients() error {
	if s.store == nil {
		return nil
	}

	records, err := s.store.ListClients(context.Background())
	if err != nil {
		return err
	}

	for _, record := range records {
		client := clientFromRecord(record)
		s.clients[client.ID] = client
		s.clientSeq = maxPrefixedSequence(s.clientSeq, "client", client.ID)

		assignments, err := s.store.ListClientAssignments(context.Background(), client.ID)
		if err != nil {
			return err
		}
		s.clientAssignments[client.ID] = make([]managedClientAssignment, 0, len(assignments))
		for _, assignmentRecord := range assignments {
			assignment := clientAssignmentFromRecord(assignmentRecord)
			s.clientAssignments[client.ID] = append(s.clientAssignments[client.ID], assignment)
			s.assignmentSeq = maxPrefixedSequence(s.assignmentSeq, "client-assignment", assignment.ID)
		}

		deployments, err := s.store.ListClientDeployments(context.Background(), client.ID)
		if err != nil {
			return err
		}
		if s.clientDeployments[client.ID] == nil {
			s.clientDeployments[client.ID] = make(map[string]managedClientDeployment)
		}
		for _, deploymentRecord := range deployments {
			deployment := clientDeploymentFromRecord(deploymentRecord)
			s.clientDeployments[client.ID][deployment.AgentID] = deployment
		}
	}

	return nil
}

func (s *Server) listClientsSnapshot() []managedClient {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	result := make([]managedClient, 0, len(s.clients))
	for _, client := range s.clients {
		if client.DeletedAt != nil {
			continue
		}
		result = append(result, client)
	}

	sort.Slice(result, func(left int, right int) bool {
		if result[left].CreatedAt.Equal(result[right].CreatedAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].CreatedAt.Before(result[right].CreatedAt)
	})

	return result
}

func (s *Server) clientDetailSnapshot(clientID string) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	client, ok := s.clients[clientID]
	if !ok {
		return managedClient{}, nil, nil, storage.ErrNotFound
	}

	assignments := append([]managedClientAssignment(nil), s.clientAssignments[clientID]...)
	sort.Slice(assignments, func(left int, right int) bool {
		if assignments[left].CreatedAt.Equal(assignments[right].CreatedAt) {
			return assignments[left].ID < assignments[right].ID
		}
		return assignments[left].CreatedAt.Before(assignments[right].CreatedAt)
	})

	deploymentsMap := s.clientDeployments[clientID]
	deployments := make([]managedClientDeployment, 0, len(deploymentsMap))
	for _, deployment := range deploymentsMap {
		deployments = append(deployments, deployment)
	}
	sort.Slice(deployments, func(left int, right int) bool {
		return deployments[left].AgentID < deployments[right].AgentID
	})

	return client, assignments, deployments, nil
}

func (s *Server) createClient(actorID string, input clientMutationInput, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
	return s.createClientWithContext(context.Background(), actorID, input, observedAt)
}

func (s *Server) createClientWithContext(ctx context.Context, actorID string, input clientMutationInput, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
	observedAt = observedAt.UTC()

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return managedClient{}, nil, nil, errClientNameRequired
	}

	userADTag, err := resolvedUserADTag(input.UserADTag, "")
	if err != nil {
		return managedClient{}, nil, nil, err
	}

	secret := strings.TrimSpace(input.Secret)
	if secret != "" {
		if !isValidHexSecret(secret) {
			return managedClient{}, nil, nil, fmt.Errorf("invalid secret format: must be 32 hex characters")
		}
	} else {
		secret, err = randomHexString(16)
		if err != nil {
			return managedClient{}, nil, nil, err
		}
	}

	expirationRFC3339, err := normalizedExpiration(input.ExpirationRFC3339)
	if err != nil {
		return managedClient{}, nil, nil, err
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	client := managedClient{
		ID:                s.nextClientID(),
		Name:              name,
		Secret:            secret,
		UserADTag:         userADTag,
		Enabled:           enabled,
		MaxTCPConns:       input.MaxTCPConns,
		MaxUniqueIPs:      input.MaxUniqueIPs,
		DataQuotaBytes:    input.DataQuotaBytes,
		ExpirationRFC3339: expirationRFC3339,
		CreatedAt:         observedAt,
		UpdatedAt:         observedAt,
	}

	assignments := s.buildClientAssignments(client.ID, input, observedAt)
	targetAgentIDs := s.resolveClientTargetAgentIDs(assignments)
	if len(targetAgentIDs) == 0 {
		return managedClient{}, nil, nil, errClientTargetsRequired
	}

	deployments := buildClientDeployments(nil, client.ID, targetAgentIDs, string(jobs.ActionClientCreate), observedAt)
	// Persist client state before enqueuing the job so a failure in
	// persistence does not leave a dispatched job referencing unknown state.
	if err := s.replaceClientStateWithContext(ctx, client, assignments, deployments); err != nil {
		return managedClient{}, nil, nil, err
	}
	if _, err := s.enqueueClientJob(actorID, jobs.ActionClientCreate, client, "", targetAgentIDs, observedAt); err != nil {
		return managedClient{}, nil, nil, err
	}

	return client, assignments, deployments, nil
}

func (s *Server) updateClient(clientID string, actorID string, input clientMutationInput, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
	return s.updateClientWithContext(context.Background(), clientID, actorID, input, observedAt)
}

func (s *Server) updateClientWithContext(ctx context.Context, clientID string, actorID string, input clientMutationInput, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
	observedAt = observedAt.UTC()

	currentClient, _, currentDeployments, err := s.clientDetailSnapshot(clientID)
	if err != nil {
		return managedClient{}, nil, nil, err
	}
	if currentClient.DeletedAt != nil {
		return managedClient{}, nil, nil, storage.ErrNotFound
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return managedClient{}, nil, nil, errClientNameRequired
	}

	userADTag, err := resolvedUserADTag(input.UserADTag, currentClient.UserADTag)
	if err != nil {
		return managedClient{}, nil, nil, err
	}

	expirationRFC3339, err := normalizedExpiration(input.ExpirationRFC3339)
	if err != nil {
		return managedClient{}, nil, nil, err
	}

	enabled := currentClient.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	previousName := currentClient.Name
	currentClient.Name = name
	currentClient.UserADTag = userADTag
	currentClient.Enabled = enabled
	currentClient.MaxTCPConns = input.MaxTCPConns
	currentClient.MaxUniqueIPs = input.MaxUniqueIPs
	currentClient.DataQuotaBytes = input.DataQuotaBytes
	currentClient.ExpirationRFC3339 = expirationRFC3339
	currentClient.UpdatedAt = observedAt

	assignments := s.buildClientAssignments(clientID, input, observedAt)
	targetAgentIDs := s.resolveClientTargetAgentIDs(assignments)
	deployments := buildClientDeployments(currentDeployments, clientID, targetAgentIDs, string(jobs.ActionClientUpdate), observedAt)

	// Persist client state before enqueuing jobs so a failure in
	// persistence does not leave dispatched jobs referencing stale state.
	if err := s.replaceClientStateWithContext(ctx, currentClient, assignments, deployments); err != nil {
		return managedClient{}, nil, nil, err
	}

	if len(targetAgentIDs) > 0 {
		if _, err := s.enqueueClientJob(actorID, jobs.ActionClientUpdate, currentClient, previousName, targetAgentIDs, observedAt); err != nil {
			return managedClient{}, nil, nil, err
		}
	}

	removedAgentIDs := removedClientTargetAgentIDs(currentDeployments, targetAgentIDs)
	if len(removedAgentIDs) > 0 {
		if _, err := s.enqueueClientJob(actorID, jobs.ActionClientDelete, currentClient, "", removedAgentIDs, observedAt); err != nil {
			return managedClient{}, nil, nil, err
		}
	}

	return currentClient, assignments, deployments, nil
}

func (s *Server) rotateClientSecret(clientID string, actorID string, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
	return s.rotateClientSecretWithContext(context.Background(), clientID, actorID, observedAt)
}

func (s *Server) rotateClientSecretWithContext(ctx context.Context, clientID string, actorID string, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
	observedAt = observedAt.UTC()

	currentClient, assignments, deployments, err := s.clientDetailSnapshot(clientID)
	if err != nil {
		return managedClient{}, nil, nil, err
	}
	if currentClient.DeletedAt != nil {
		return managedClient{}, nil, nil, storage.ErrNotFound
	}

	secret, err := randomHexString(16)
	if err != nil {
		return managedClient{}, nil, nil, err
	}
	currentClient.Secret = secret
	currentClient.UpdatedAt = observedAt

	targetAgentIDs := s.resolveClientTargetAgentIDs(assignments)
	deployments = buildClientDeployments(deployments, clientID, targetAgentIDs, string(jobs.ActionClientRotateSecret), observedAt)
	// Persist the new secret before enqueuing the rotation job so a
	// persistence failure does not leave a dispatched job with a secret
	// the control-plane never recorded.
	if err := s.replaceClientStateWithContext(ctx, currentClient, assignments, deployments); err != nil {
		return managedClient{}, nil, nil, err
	}
	if len(targetAgentIDs) > 0 {
		if _, err := s.enqueueClientJob(actorID, jobs.ActionClientRotateSecret, currentClient, "", targetAgentIDs, observedAt); err != nil {
			return managedClient{}, nil, nil, err
		}
	}

	return currentClient, assignments, deployments, nil
}

func (s *Server) deleteClient(clientID string, actorID string, observedAt time.Time) error {
	return s.deleteClientWithContext(context.Background(), clientID, actorID, observedAt)
}

func (s *Server) deleteClientWithContext(ctx context.Context, clientID string, actorID string, observedAt time.Time) error {
	observedAt = observedAt.UTC()

	currentClient, assignments, deployments, err := s.clientDetailSnapshot(clientID)
	if err != nil {
		return err
	}
	if currentClient.DeletedAt != nil {
		return storage.ErrNotFound
	}

	currentClient.Enabled = false
	currentClient.UpdatedAt = observedAt
	currentClient.DeletedAt = &observedAt

	targetAgentIDs := s.resolveClientTargetAgentIDs(assignments)
	if len(targetAgentIDs) == 0 {
		targetAgentIDs = deploymentAgentIDs(deployments)
	}
	deployments = buildClientDeployments(deployments, clientID, targetAgentIDs, string(jobs.ActionClientDelete), observedAt)

	// Persist the tombstone before dispatching the delete job so a persistence
	// failure does not leave the agent with a removed client while the DB
	// record still shows DeletedAt=nil (ghost state, see P2-LOG-01 / M-C1).
	if err := s.replaceClientStateWithContext(ctx, currentClient, assignments, deployments); err != nil {
		return err
	}

	if len(targetAgentIDs) > 0 {
		if _, err := s.enqueueClientJob(actorID, jobs.ActionClientDelete, currentClient, "", targetAgentIDs, observedAt); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) enqueueClientJob(actorID string, action jobs.Action, client managedClient, previousName string, targetAgentIDs []string, observedAt time.Time) (jobs.Job, error) {
	payloadJSON, err := json.Marshal(clientJobPayload{
		ClientID:          client.ID,
		PreviousName:      previousName,
		Name:              client.Name,
		Secret:            client.Secret,
		UserADTag:         client.UserADTag,
		Enabled:           client.Enabled,
		MaxTCPConns:       client.MaxTCPConns,
		MaxUniqueIPs:      client.MaxUniqueIPs,
		DataQuotaBytes:    client.DataQuotaBytes,
		ExpirationRFC3339: client.ExpirationRFC3339,
	})
	if err != nil {
		return jobs.Job{}, err
	}

	readOnlyAgents := make(map[string]bool, len(targetAgentIDs))
	s.mu.RLock()
	for _, agentID := range targetAgentIDs {
		agent, ok := s.agents[agentID]
		if ok {
			readOnlyAgents[agentID] = agent.ReadOnly
		}
	}
	s.mu.RUnlock()

	job, err := s.jobs.Enqueue(jobs.CreateJobInput{
		Action:         action,
		TargetAgentIDs: targetAgentIDs,
		TTL:            clientJobTTL,
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", action, client.ID, observedAt.UnixNano()),
		ActorID:        actorID,
		ReadOnlyAgents: readOnlyAgents,
		PayloadJSON:    string(payloadJSON),
	}, observedAt)
	if err != nil {
		return jobs.Job{}, err
	}
	s.notifyAgentSessions(job.TargetAgentIDs)

	return job, nil
}

func (s *Server) replaceClientState(client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) error {
	return s.replaceClientStateWithContext(context.Background(), client, assignments, deployments)
}

func (s *Server) replaceClientStateWithContext(ctx context.Context, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) error {
	if s.store != nil {
		if err := s.persistClientState(ctx, client, assignments, deployments); err != nil {
			return err
		}
	}

	s.replaceClientStateInMemory(client, assignments, deployments)
	return nil
}

// replaceClientStateInMemory updates the in-memory mirror of client
// state without touching the store. Factored out of
// replaceClientStateWithContext so callers that drive persistence
// through Store.Transact can apply the in-memory update only after the
// transaction commits (see adoptDiscoveredClient, P2-ARCH-01).
func (s *Server) replaceClientStateInMemory(client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients[client.ID] = client
	s.clientAssignments[client.ID] = append([]managedClientAssignment(nil), assignments...)
	nextDeployments := make(map[string]managedClientDeployment, len(deployments))
	for _, deployment := range deployments {
		nextDeployments[deployment.AgentID] = deployment
	}
	s.clientDeployments[client.ID] = nextDeployments
}

func (s *Server) persistClientState(ctx context.Context, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) error {
	return persistClientStateVia(ctx, s.store, client, assignments, deployments)
}

// persistClientStateVia delegates to clients.PersistState. Kept as a
// server-package shim so call sites inside Store.Transact closures
// continue to read idiomatically (P2-ARCH-01). Will be removed once
// callers invoke clients.PersistState directly.
func persistClientStateVia(ctx context.Context, store storage.Store, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) error {
	return clients.PersistState(ctx, store, client, assignments, deployments)
}

func (s *Server) buildClientAssignments(clientID string, input clientMutationInput, observedAt time.Time) []managedClientAssignment {
	assignments := make([]managedClientAssignment, 0, len(input.FleetGroupIDs)+len(input.AgentIDs))
	for _, fleetGroupID := range normalizedIDs(input.FleetGroupIDs) {
		assignments = append(assignments, managedClientAssignment{
			ID:           s.nextClientAssignmentID(),
			ClientID:     clientID,
			TargetType:   clientAssignmentTargetFleetGroup,
			FleetGroupID: fleetGroupID,
			CreatedAt:    observedAt,
		})
	}
	for _, agentID := range normalizedIDs(input.AgentIDs) {
		assignments = append(assignments, managedClientAssignment{
			ID:         s.nextClientAssignmentID(),
			ClientID:   clientID,
			TargetType: clientAssignmentTargetAgent,
			AgentID:    agentID,
			CreatedAt:  observedAt,
		})
	}

	return assignments
}

// resolveClientTargetAgentIDs maps a slice of client assignments to the
// concrete set of agent IDs they currently resolve to.
//
// Lock discipline (P2-LOG-11 / M-C11 / L-08): callers typically obtain
// `assignments` under s.clientsMu. We MUST NOT hold s.clientsMu while
// taking s.mu (that would invert the mu -> clientsMu ordering observed
// by applyAgentSnapshot and would deadlock). To keep the two lock windows
// disjoint AND avoid iterating s.agents while holding s.mu for the full
// loop body, we snapshot only the fields needed for resolution (agent ID
// and fleet-group ID) into local maps, release s.mu, and iterate the
// caller-provided assignments against those local snapshots.
//
// The snapshot can race with a concurrent agent mutation, but callers
// already tolerate that: the result is used to build deployment rows that
// are re-reconciled on the next snapshot. The race is therefore benign
// and, crucially, lock-order-safe.
// resolveClientTargetAgentIDs snapshots the current agent topology
// under s.mu and delegates the deterministic deduplication + sorting
// to clients.Service.ResolveTargetAgentIDs.
//
// Lock discipline (P2-LOG-11 / M-C11 / L-08): callers typically obtain
// `assignments` under s.clientsMu. We MUST NOT hold s.clientsMu while
// taking s.mu (that would invert the documented ordering). To keep
// the two locks disjoint AND avoid iterating s.agents while holding
// s.mu for the full target computation, snapshot the registered-agent
// IDs and fleet-group membership into local maps, release s.mu, and
// let the pure helper iterate against those local snapshots.
func (s *Server) resolveClientTargetAgentIDs(assignments []managedClientAssignment) []string {
	s.mu.RLock()
	registeredAgents := make(map[string]struct{}, len(s.agents))
	fleetMembers := make(map[string][]string)
	for _, agent := range s.agents {
		registeredAgents[agent.ID] = struct{}{}
		if agent.FleetGroupID != "" {
			fleetMembers[agent.FleetGroupID] = append(fleetMembers[agent.FleetGroupID], agent.ID)
		}
	}
	s.mu.RUnlock()

	return s.clientsSvc.ResolveTargetAgentIDs(assignments, clients.AgentTopology{
		RegisteredAgents: registeredAgents,
		FleetMembers:     fleetMembers,
	})
}

func (s *Server) recordClientJobResult(agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
	s.recordClientJobResultWithContext(context.Background(), agentID, jobID, success, message, resultJSON, observedAt)
}

func (s *Server) recordClientJobResultWithContext(ctx context.Context, agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
	job, ok := s.jobByID(jobID)
	if !ok {
		return
	}

	switch job.Action {
	case jobs.ActionClientCreate, jobs.ActionClientUpdate, jobs.ActionClientDelete, jobs.ActionClientRotateSecret:
	default:
		return
	}

	var payload clientJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return
	}

	s.clientsMu.Lock()
	client, ok := s.clients[payload.ClientID]
	if !ok {
		s.clientsMu.Unlock()
		return
	}
	deployment := s.clientDeployments[payload.ClientID][agentID]

	deployment.ClientID = payload.ClientID
	deployment.AgentID = agentID
	deployment.DesiredOperation = string(job.Action)
	deployment.UpdatedAt = observedAt.UTC()
	if success {
		deployment.Status = clientDeploymentStatusSucceeded
		deployment.LastError = ""
		lastAppliedAt := observedAt.UTC()
		deployment.LastAppliedAt = &lastAppliedAt

		if job.Action == jobs.ActionClientDelete {
			deployment.ConnectionLink = ""
		} else if strings.TrimSpace(resultJSON) != "" {
			var resultPayload clientJobResultPayload
			if err := json.Unmarshal([]byte(resultJSON), &resultPayload); err == nil && resultPayload.ConnectionLink != "" {
				deployment.ConnectionLink = resultPayload.ConnectionLink
			}
		}
	} else {
		deployment.Status = clientDeploymentStatusFailed
		deployment.LastError = message
	}

	if s.clientDeployments[payload.ClientID] == nil {
		s.clientDeployments[payload.ClientID] = make(map[string]managedClientDeployment)
	}
	s.clientDeployments[payload.ClientID][agentID] = deployment
	s.clients[payload.ClientID] = client
	s.clientsMu.Unlock()

	if s.store != nil {
		if err := s.store.PutClientDeployment(ctx, clientDeploymentToRecord(deployment)); err != nil {
			s.logger.Error("client deployment persistence failed", "client_id", payload.ClientID, "agent_id", agentID, "error", err)
		}
	}
}

func (s *Server) jobByID(jobID string) (jobs.Job, bool) {
	for _, job := range s.jobs.List() {
		if job.ID == jobID {
			return job, true
		}
	}

	return jobs.Job{}, false
}

// aggregatedClientUsage delegates the sum-over-agents computation to
// clients.AggregateUsage. The server still owns the in-memory usage
// map (migrating that off Server is tracked as future follow-up work)
// so we snapshot + release before calling into the pure helper.
func (s *Server) aggregatedClientUsage(clientID string) aggregatedClientUsage {
	s.clientsMu.RLock()
	usageByAgent := s.clientUsage[clientID]
	snapshot := make(map[string]clients.UsageSnapshot, len(usageByAgent))
	for agentID, value := range usageByAgent {
		snapshot[agentID] = value
	}
	s.clientsMu.RUnlock()

	return s.clientsSvc.AggregateUsage(snapshot)
}

// resolveClientIDByName finds the panel client ID for a given client name
// assigned to a specific agent. Used when the agent sends usage snapshots
// without a panel-assigned client_id (e.g. adopted clients).
//
// A client matches when it is either directly assigned to the agent OR
// assigned to a fleet group the agent belongs to (P2-LOG-07 / M-C3). Without
// the fleet-group fallback, usage stats for clients attached via fleet-group
// assignments were silently dropped.
// resolveClientIDByName snapshots the agent's current fleet group under
// s.mu then delegates the name lookup to clients.Service.ResolveIDByName.
// The two locks (s.mu and s.clientsMu) are never held together, which
// preserves the documented lock ordering.
func (s *Server) resolveClientIDByName(agentID string, clientName string) string {
	s.mu.RLock()
	agentFleetGroupID := ""
	if agent, ok := s.agents[agentID]; ok {
		agentFleetGroupID = agent.FleetGroupID
	}
	s.mu.RUnlock()

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	return s.clientsSvc.ResolveIDByName(s.clients, s.clientAssignments, agentID, agentFleetGroupID, clientName)
}

func (s *Server) nextClientID() string {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	s.clientSeq++
	return newSequenceID("client", s.clientSeq)
}

func (s *Server) nextClientAssignmentID() string {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	s.assignmentSeq++
	return newSequenceID("client-assignment", s.assignmentSeq)
}

// buildClientDeployments delegates to clients.BuildDeployments.
// Agents no longer in the target set are marked for deletion; see
// deployments.go in the clients package.
func buildClientDeployments(current []managedClientDeployment, clientID string, targetAgentIDs []string, desiredOperation string, observedAt time.Time) []managedClientDeployment {
	return clients.BuildDeployments(current, clientID, targetAgentIDs, desiredOperation, string(jobs.ActionClientDelete), observedAt)
}

// removedClientTargetAgentIDs delegates to clients.RemovedTargetAgentIDs.
func removedClientTargetAgentIDs(current []managedClientDeployment, next []string) []string {
	return clients.RemovedTargetAgentIDs(current, next)
}

// deploymentAgentIDs delegates to clients.DeploymentAgentIDs.
func deploymentAgentIDs(deployments []managedClientDeployment) []string {
	return clients.DeploymentAgentIDs(deployments)
}

// normalizedIDs delegates to clients.NormalizedIDs.
func normalizedIDs(values []string) []string {
	return clients.NormalizedIDs(values)
}

// resolvedUserADTag delegates to clients.ResolveUserADTag, translating
// the sentinel error into the server-package sentinel so existing
// errors.Is call sites still match.
func resolvedUserADTag(value string, fallback string) (string, error) {
	tag, err := clients.ResolveUserADTag(value, fallback)
	if errors.Is(err, clients.ErrUserADTag) {
		return "", errClientUserADTag
	}
	return tag, err
}

// normalizedExpiration delegates to clients.NormalizeExpiration.
func normalizedExpiration(value string) (string, error) {
	out, err := clients.NormalizeExpiration(value)
	if errors.Is(err, clients.ErrExpiration) {
		return "", errClientExpiration
	}
	return out, err
}

// randomHexString delegates to clients.RandomHexString.
func randomHexString(size int) (string, error) {
	return clients.RandomHexString(size)
}

// isValidHexSecret delegates to clients.IsValidHexSecret.
func isValidHexSecret(s string) bool {
	return clients.IsValidHexSecret(s)
}
