package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
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
	errClientNameRequired    = errors.New("client name is required")
	errClientNameInvalid     = errors.New("client name must match [A-Za-z0-9_.-] and be 1..64 chars")
	errClientUserADTag       = errors.New("user_ad_tag must contain exactly 32 hex characters")
	errClientExpiration      = errors.New("expiration_rfc3339 must be a valid RFC3339 timestamp")
	errClientTargetsRequired = errors.New("client must target at least one agent")
)

// clientNameRegex mirrors Telemt's username constraint
// (telemt-server: username must match [A-Za-z0-9_.-] and be 1..64 chars).
// The panel rejects mismatches up-front so an operator never ends up
// with a control-plane row whose rollout job is guaranteed to fail on
// every agent.
var clientNameRegex = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// clientJobTTL is the compiled-in default TTL for client-mutation jobs.
// The live value is resolved via s.effectiveClientJobTTL() so operator
// changes to jobs.client_job_ttl take effect without a panel restart.
const clientJobTTL = 10 * time.Minute

// effectiveClientJobTTL returns the current client-job TTL. When the
// operational settings store is wired, the live value is used; falls back
// to the compiled-in constant otherwise.
func (s *Server) effectiveClientJobTTL() time.Duration {
	if s.settings != nil {
		return s.settings.JobsClientJobTTL()
	}
	return clientJobTTL
}

type clientMutationInput struct {
	Name      string
	Secret    string
	Enabled   *bool
	UserADTag string
	// UserADTagAuto is a tri-state flag:
	//   * nil                 → legacy behaviour (empty tag auto-gens
	//                            on create / keeps current on update)
	//   * ptr-to-true         → same as legacy; accepted for explicitness
	//   * ptr-to-false        → use UserADTag literally; empty stores empty
	// Callers parse the HTTP `user_ad_tag_auto` field into this pointer.
	UserADTagAuto     *bool
	MaxTCPConns       int
	MaxUniqueIPs      int
	DataQuotaBytes    int64
	ExpirationRFC3339 string
	FleetGroupIDs     []string
	AgentIDs          []string
}

type clientJobPayload struct {
	ClientID          string `json:"client_id"`
	PreviousName      string `json:"previous_name,omitempty"`
	Name              string `json:"name"`
	Secret            string `json:"secret"`
	UserADTag         string `json:"user_ad_tag"`
	Enabled           bool   `json:"enabled"`
	MaxTCPConns       int    `json:"max_tcp_conns"`
	MaxUniqueIPs      int    `json:"max_unique_ips"`
	DataQuotaBytes    int64  `json:"data_quota_bytes"`
	ExpirationRFC3339 string `json:"expiration_rfc3339"`
}

type clientJobResultPayload struct {
	ConnectionLinks []string `json:"connection_links"`
}

// aggregatedClientUsage now lives in controlplane/clients as
// AggregatedUsage. Kept as a server-local alias so existing call sites
// (HTTP response composition, test assertions) keep compiling until
// they are renamed to use the clients package directly.
type aggregatedClientUsage = clients.AggregatedUsage

func (s *Server) createClient(ctx context.Context, actorID string, input clientMutationInput, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
	observedAt = observedAt.UTC()

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return managedClient{}, nil, nil, errClientNameRequired
	}
	if !clientNameRegex.MatchString(name) {
		return managedClient{}, nil, nil, errClientNameInvalid
	}

	userADTag, err := resolveUserADTagForMutation(input, "")
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
	if _, err := s.enqueueClientJob(ctx, actorID, jobs.ActionClientCreate, client, "", targetAgentIDs, observedAt); err != nil {
		return managedClient{}, nil, nil, err
	}

	return client, assignments, deployments, nil
}

func (s *Server) updateClient(ctx context.Context, clientID, actorID string, input clientMutationInput, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
	observedAt = observedAt.UTC()

	currentClient, _, currentDeployments, err := s.clientDetailSnapshot(clientID)
	if err != nil {
		return managedClient{}, nil, nil, err
	}
	if currentClient.DeletedAt != nil {
		return managedClient{}, nil, nil, storage.ErrNotFound
	}

	previousName, err := applyClientMutationFields(&currentClient, input, observedAt)
	if err != nil {
		return managedClient{}, nil, nil, err
	}

	assignments := s.buildClientAssignments(clients.ClientID(clientID), input, observedAt)
	targetAgentIDs := s.resolveClientTargetAgentIDs(assignments)
	deployments := buildClientDeployments(currentDeployments, clients.ClientID(clientID), targetAgentIDs, string(jobs.ActionClientUpdate), observedAt)

	// Persist client state before enqueuing jobs so a failure in
	// persistence does not leave dispatched jobs referencing stale state.
	if err := s.replaceClientStateWithContext(ctx, currentClient, assignments, deployments); err != nil {
		return managedClient{}, nil, nil, err
	}

	if err := s.dispatchClientUpdateJobs(ctx, actorID, currentClient, previousName, currentDeployments, targetAgentIDs, observedAt); err != nil {
		return managedClient{}, nil, nil, err
	}

	return currentClient, assignments, deployments, nil
}

// applyClientMutationFields validates the mutation input and merges it
// into currentClient in-place. Returns the pre-mutation Name (used for
// rename detection in the apply flow) or any validation error.
func applyClientMutationFields(currentClient *managedClient, input clientMutationInput, observedAt time.Time) (string, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return "", errClientNameRequired
	}
	if !clientNameRegex.MatchString(name) {
		return "", errClientNameInvalid
	}

	userADTag, err := resolveUserADTagForMutation(input, currentClient.UserADTag)
	if err != nil {
		return "", err
	}

	expirationRFC3339, err := normalizedExpiration(input.ExpirationRFC3339)
	if err != nil {
		return "", err
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
	return previousName, nil
}

// redeployClientWithContext re-queues the create job for every target
// agent on the client. Used to recover a client whose initial rollout
// partially or fully failed — the panel still has the record, but one
// or more Telemt nodes rejected the apply (bad ad tag, network blip,
// etc.). Re-running the flow with the current stored state is the
// operator-facing equivalent of "retry deployment".
func (s *Server) redeployClientWithContext(ctx context.Context, clientID, actorID string, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
	observedAt = observedAt.UTC()

	currentClient, assignments, deployments, err := s.clientDetailSnapshot(clientID)
	if err != nil {
		return managedClient{}, nil, nil, err
	}
	if currentClient.DeletedAt != nil {
		return managedClient{}, nil, nil, storage.ErrNotFound
	}

	targetAgentIDs := s.resolveClientTargetAgentIDs(assignments)
	if len(targetAgentIDs) == 0 {
		// No targets at all — nothing to redeploy. Return current state
		// so the caller surfaces "no-op" gracefully rather than looking
		// like a silent success.
		return currentClient, assignments, deployments, nil
	}

	deployments = buildClientDeployments(deployments, clients.ClientID(clientID), targetAgentIDs, string(jobs.ActionClientCreate), observedAt)
	if err := s.replaceClientStateWithContext(ctx, currentClient, assignments, deployments); err != nil {
		return managedClient{}, nil, nil, err
	}
	if _, err := s.enqueueClientJob(ctx, actorID, jobs.ActionClientCreate, currentClient, "", targetAgentIDs, observedAt); err != nil {
		return managedClient{}, nil, nil, err
	}
	return currentClient, assignments, deployments, nil
}

func (s *Server) rotateClientSecret(ctx context.Context, clientID, actorID string, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
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
	deployments = buildClientDeployments(deployments, clients.ClientID(clientID), targetAgentIDs, string(jobs.ActionClientRotateSecret), observedAt)
	// Persist the new secret before enqueuing the rotation job so a
	// persistence failure does not leave a dispatched job with a secret
	// the control-plane never recorded.
	if err := s.replaceClientStateWithContext(ctx, currentClient, assignments, deployments); err != nil {
		return managedClient{}, nil, nil, err
	}
	if len(targetAgentIDs) > 0 {
		if _, err := s.enqueueClientJob(ctx, actorID, jobs.ActionClientRotateSecret, currentClient, "", targetAgentIDs, observedAt); err != nil {
			return managedClient{}, nil, nil, err
		}
	}

	return currentClient, assignments, deployments, nil
}

func (s *Server) deleteClient(ctx context.Context, clientID, actorID string, observedAt time.Time) error {
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
	deployments = buildClientDeployments(deployments, clients.ClientID(clientID), targetAgentIDs, string(jobs.ActionClientDelete), observedAt)

	// Persist the tombstone before dispatching the delete job so a persistence
	// failure does not leave the agent with a removed client while the DB
	// record still shows DeletedAt=nil (ghost state, see P2-LOG-01 / M-C1).
	if err := s.replaceClientStateWithContext(ctx, currentClient, assignments, deployments); err != nil {
		return err
	}

	if len(targetAgentIDs) > 0 {
		if _, err := s.enqueueClientJob(ctx, actorID, jobs.ActionClientDelete, currentClient, "", targetAgentIDs, observedAt); err != nil {
			return err
		}
	}

	return nil
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
	s.clients[string(client.ID)] = client
	s.clientAssignments[string(client.ID)] = append([]managedClientAssignment(nil), assignments...)
	nextDeployments := make(map[string]managedClientDeployment, len(deployments))
	for _, deployment := range deployments {
		nextDeployments[deployment.AgentID] = deployment
	}
	s.clientDeployments[string(client.ID)] = nextDeployments
}

func (s *Server) persistClientState(ctx context.Context, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) error {
	return persistClientStateVia(ctx, s.store, client, assignments, deployments, s.vault())
}

// persistClientStateVia delegates to clients.PersistState. Kept as a
// server-package shim so call sites inside Store.Transact closures
// continue to read idiomatically (P2-ARCH-01). Will be removed once
// callers invoke clients.PersistState directly.
func persistClientStateVia(ctx context.Context, store storage.Store, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment, vault *secretvault.Vault) error {
	return clients.PersistState(ctx, store, client, assignments, deployments, vault)
}

func (s *Server) buildClientAssignments(clientID clients.ClientID, input clientMutationInput, observedAt time.Time) []managedClientAssignment {
	assignments := make([]managedClientAssignment, 0, len(input.FleetGroupIDs)+len(input.AgentIDs))
	for _, fleetGroupID := range normalizedIDs(input.FleetGroupIDs) {
		assignments = append(assignments, managedClientAssignment{
			ID:           s.nextClientAssignmentID(),
			ClientID:     clientID,
			TargetType:   clientAssignmentTargetFleetGroup,
			FleetGroupID: clients.FleetGroupID(fleetGroupID),
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

func (s *Server) recordClientJobResultWithContext(ctx context.Context, agentID, jobID string, success bool, message, resultJSON string, observedAt time.Time) {
	job, ok := s.jobByID(ctx, jobID)
	if !ok {
		return
	}
	if !isClientJobAction(job.Action) {
		return
	}

	var payload clientJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return
	}

	deployment, ok := s.applyClientJobDeployment(payload.ClientID, agentID, job, success, message, resultJSON, observedAt)
	if !ok {
		return
	}

	if s.clientsSvc != nil {
		if err := s.clientsSvc.PersistDeployment(ctx, deployment); err != nil {
			s.logger.Error("client deployment persistence failed", "client_id", payload.ClientID, "agent_id", agentID, "error", err)
		}
	}
}

func isClientJobAction(action jobs.Action) bool {
	switch action {
	case jobs.ActionClientCreate, jobs.ActionClientUpdate, jobs.ActionClientDelete, jobs.ActionClientRotateSecret:
		return true
	default:
		return false
	}
}

// applyClientJobDeployment updates the in-memory deployment state for a
// client job result and returns the updated deployment. Returns ok=false
// when the client is no longer tracked.
func (s *Server) applyClientJobDeployment(clientID, agentID string, job jobs.Job, success bool, message, resultJSON string, observedAt time.Time) (managedClientDeployment, bool) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	client, ok := s.clients[clientID]
	if !ok {
		return managedClientDeployment{}, false
	}
	deployment := s.clientDeployments[clientID][agentID]

	deployment.ClientID = clients.ClientID(clientID)
	deployment.AgentID = agentID
	deployment.DesiredOperation = string(job.Action)
	deployment.UpdatedAt = observedAt.UTC()
	applyClientJobOutcome(&deployment, job.Action, success, message, resultJSON, observedAt)

	if s.clientDeployments[clientID] == nil {
		s.clientDeployments[clientID] = make(map[string]managedClientDeployment)
	}
	s.clientDeployments[clientID][agentID] = deployment
	s.clients[clientID] = client
	return deployment, true
}

func applyClientJobOutcome(deployment *managedClientDeployment, action jobs.Action, success bool, message, resultJSON string, observedAt time.Time) {
	if !success {
		deployment.Status = clientDeploymentStatusFailed
		deployment.LastError = message
		return
	}
	deployment.Status = clientDeploymentStatusSucceeded
	deployment.LastError = ""
	lastAppliedAt := observedAt.UTC()
	deployment.LastAppliedAt = &lastAppliedAt

	if action == jobs.ActionClientDelete {
		deployment.ConnectionLinks = nil
		return
	}
	if strings.TrimSpace(resultJSON) == "" {
		return
	}
	var resultPayload clientJobResultPayload
	if err := json.Unmarshal([]byte(resultJSON), &resultPayload); err == nil && len(resultPayload.ConnectionLinks) > 0 {
		deployment.ConnectionLinks = resultPayload.ConnectionLinks
	}
}

// jobByID returns the job with the given ID. P-4: backed by the O(1)
// jobs.Service.Get index — historically this iterated ListWithContext,
// which was O(jobs) per result-recording call.
func (s *Server) jobByID(_ context.Context, jobID string) (jobs.Job, bool) {
	return s.jobs.Get(jobID)
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
func (s *Server) resolveClientIDByName(agentID, clientName string) string {
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

func (s *Server) nextClientID() clients.ClientID {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	s.clientSeq++
	return clients.ClientID(newSequenceID("client", s.clientSeq))
}

func (s *Server) nextClientAssignmentID() clients.AssignmentID {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	s.assignmentSeq++
	return clients.AssignmentID(newSequenceID("client-assignment", s.assignmentSeq))
}

// buildClientDeployments delegates to clients.BuildDeployments.
// Agents no longer in the target set are marked for deletion; see
// deployments.go in the clients package.
func buildClientDeployments(current []managedClientDeployment, clientID clients.ClientID, targetAgentIDs []string, desiredOperation string, observedAt time.Time) []managedClientDeployment {
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
func resolvedUserADTag(value, fallback string) (string, error) {
	tag, err := clients.ResolveUserADTag(value, fallback)
	if errors.Is(err, clients.ErrUserADTag) {
		return "", errClientUserADTag
	}
	return tag, err
}

// resolveUserADTagForMutation honours the tri-state
// clientMutationInput.UserADTagAuto flag:
//   - nil or *true  → legacy auto-gen / fallback behaviour.
//   - *false        → operator explicitly opted out of auto-gen;
//     empty stored as empty, non-empty must be valid hex.
//
// All branches feed into the same server sentinel so downstream
// errors.Is checks keep working.
func resolveUserADTagForMutation(input clientMutationInput, fallback string) (string, error) {
	if input.UserADTagAuto != nil && !*input.UserADTagAuto {
		tag, err := clients.ResolveUserADTagExplicit(input.UserADTag)
		if errors.Is(err, clients.ErrUserADTag) {
			return "", errClientUserADTag
		}
		return tag, err
	}
	return resolvedUserADTag(input.UserADTag, fallback)
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
