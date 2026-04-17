package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

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

type aggregatedClientUsage struct {
	TrafficUsedBytes uint64
	UniqueIPsUsed    int
	ActiveTCPConns   int
}

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

	s.clientsMu.Lock()
	s.clients[client.ID] = client
	s.clientAssignments[client.ID] = append([]managedClientAssignment(nil), assignments...)
	nextDeployments := make(map[string]managedClientDeployment, len(deployments))
	for _, deployment := range deployments {
		nextDeployments[deployment.AgentID] = deployment
	}
	s.clientDeployments[client.ID] = nextDeployments
	s.clientsMu.Unlock()

	return nil
}

func (s *Server) persistClientState(ctx context.Context, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) error {
	if err := s.store.PutClient(ctx, clientToRecord(client)); err != nil {
		return err
	}
	if err := s.store.DeleteClientAssignments(ctx, client.ID); err != nil {
		return err
	}
	for _, assignment := range assignments {
		if err := s.store.PutClientAssignment(ctx, clientAssignmentToRecord(assignment)); err != nil {
			return err
		}
	}
	for _, deployment := range deployments {
		if err := s.store.PutClientDeployment(ctx, clientDeploymentToRecord(deployment)); err != nil {
			return err
		}
	}

	return nil
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
func (s *Server) resolveClientTargetAgentIDs(assignments []managedClientAssignment) []string {
	// Snapshot agents under s.mu then release before iterating assignments.
	// agentsByID exists only to answer "is this agentID still registered?"
	// for direct-agent assignments; fleetMembers maps fleetGroupID to the
	// set of agent IDs in that group for fleet-group assignments.
	s.mu.RLock()
	agentsByID := make(map[string]struct{}, len(s.agents))
	fleetMembers := make(map[string][]string)
	for _, agent := range s.agents {
		agentsByID[agent.ID] = struct{}{}
		if agent.FleetGroupID != "" {
			fleetMembers[agent.FleetGroupID] = append(fleetMembers[agent.FleetGroupID], agent.ID)
		}
	}
	s.mu.RUnlock()

	targetAgentIDs := make(map[string]struct{})
	for _, assignment := range assignments {
		switch assignment.TargetType {
		case clientAssignmentTargetFleetGroup:
			for _, agentID := range fleetMembers[assignment.FleetGroupID] {
				targetAgentIDs[agentID] = struct{}{}
			}
		case clientAssignmentTargetAgent:
			if _, ok := agentsByID[assignment.AgentID]; ok {
				targetAgentIDs[assignment.AgentID] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(targetAgentIDs))
	for agentID := range targetAgentIDs {
		result = append(result, agentID)
	}
	sort.Strings(result)

	return result
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

func (s *Server) aggregatedClientUsage(clientID string) aggregatedClientUsage {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	usageByAgent := s.clientUsage[clientID]
	usage := aggregatedClientUsage{}
	for _, snapshot := range usageByAgent {
		usage.TrafficUsedBytes += snapshot.TrafficUsedBytes
		usage.UniqueIPsUsed += snapshot.UniqueIPsUsed
		usage.ActiveTCPConns += snapshot.ActiveTCPConns
	}

	return usage
}

// resolveClientIDByName finds the panel client ID for a given client name
// assigned to a specific agent. Used when the agent sends usage snapshots
// without a panel-assigned client_id (e.g. adopted clients).
//
// A client matches when it is either directly assigned to the agent OR
// assigned to a fleet group the agent belongs to (P2-LOG-07 / M-C3). Without
// the fleet-group fallback, usage stats for clients attached via fleet-group
// assignments were silently dropped.
func (s *Server) resolveClientIDByName(agentID string, clientName string) string {
	// Read the agent's fleet group under s.mu (which guards s.agents) before
	// taking s.clientsMu so the two locks are never held simultaneously.
	s.mu.RLock()
	agentFleetGroupID := ""
	if agent, ok := s.agents[agentID]; ok {
		agentFleetGroupID = agent.FleetGroupID
	}
	s.mu.RUnlock()

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for clientID, client := range s.clients {
		if client.Name != clientName {
			continue
		}
		for _, assignment := range s.clientAssignments[clientID] {
			switch assignment.TargetType {
			case clientAssignmentTargetAgent:
				if assignment.AgentID == agentID {
					return clientID
				}
			case clientAssignmentTargetFleetGroup:
				if agentFleetGroupID != "" && assignment.FleetGroupID == agentFleetGroupID {
					return clientID
				}
			}
		}
	}
	return ""
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

func buildClientDeployments(current []managedClientDeployment, clientID string, targetAgentIDs []string, desiredOperation string, observedAt time.Time) []managedClientDeployment {
	currentByAgent := make(map[string]managedClientDeployment, len(current))
	for _, deployment := range current {
		currentByAgent[deployment.AgentID] = deployment
	}

	targetSet := make(map[string]struct{}, len(targetAgentIDs))
	for _, agentID := range targetAgentIDs {
		targetSet[agentID] = struct{}{}
		deployment := currentByAgent[agentID]
		deployment.ClientID = clientID
		deployment.AgentID = agentID
		deployment.DesiredOperation = desiredOperation
		deployment.Status = clientDeploymentStatusQueued
		deployment.LastError = ""
		deployment.UpdatedAt = observedAt.UTC()
		currentByAgent[agentID] = deployment
	}

	if desiredOperation != string(jobs.ActionClientDelete) {
		for agentID, deployment := range currentByAgent {
			if _, ok := targetSet[agentID]; ok {
				continue
			}
			deployment.DesiredOperation = string(jobs.ActionClientDelete)
			deployment.Status = clientDeploymentStatusQueued
			deployment.LastError = ""
			deployment.UpdatedAt = observedAt.UTC()
			currentByAgent[agentID] = deployment
		}
	}

	result := make([]managedClientDeployment, 0, len(currentByAgent))
	for _, deployment := range currentByAgent {
		result = append(result, deployment)
	}
	sort.Slice(result, func(left int, right int) bool {
		return result[left].AgentID < result[right].AgentID
	})

	return result
}

func removedClientTargetAgentIDs(current []managedClientDeployment, next []string) []string {
	nextSet := make(map[string]struct{}, len(next))
	for _, agentID := range next {
		nextSet[agentID] = struct{}{}
	}

	removed := make([]string, 0)
	for _, deployment := range current {
		if _, ok := nextSet[deployment.AgentID]; ok {
			continue
		}
		removed = append(removed, deployment.AgentID)
	}
	sort.Strings(removed)

	return removed
}

func deploymentAgentIDs(deployments []managedClientDeployment) []string {
	agentIDs := make([]string, 0, len(deployments))
	for _, deployment := range deployments {
		agentIDs = append(agentIDs, deployment.AgentID)
	}
	sort.Strings(agentIDs)
	return agentIDs
}

func normalizedIDs(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		unique[trimmed] = struct{}{}
	}

	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)

	return result
}

func resolvedUserADTag(value string, fallback string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		if fallback != "" {
			return fallback, nil
		}
		return randomHexString(16)
	}
	if len(trimmed) != 32 {
		return "", errClientUserADTag
	}
	if _, err := hex.DecodeString(trimmed); err != nil {
		return "", errClientUserADTag
	}

	return strings.ToLower(trimmed), nil
}

func normalizedExpiration(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return "", errClientExpiration
	}

	return parsed.UTC().Format(time.RFC3339), nil
}

func randomHexString(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

var hexSecret32 = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func isValidHexSecret(s string) bool {
	return hexSecret32.MatchString(s)
}
