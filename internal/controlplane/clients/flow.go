package clients

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// clientNameRegex mirrors Telemt's username constraint
// (telemt-server: username must match [A-Za-z0-9_.-] and be 1..64 chars).
// The panel rejects mismatches up-front so an operator never ends up
// with a control-plane row whose rollout job is guaranteed to fail on
// every agent.
var clientNameRegex = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// MutationInput is the validated create/update payload for a managed
// client. It is the former server.clientMutationInput, field-for-field.
type MutationInput struct {
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

// Create validates the mutation input, mints a new client (secret,
// subscription token, sequence ID), persists it before enqueuing the
// create job, and returns the resulting client/assignment/deployment
// state.
func (s *Service) Create(ctx context.Context, actorID string, input MutationInput, observedAt time.Time) (Client, []Assignment, []Deployment, error) {
	observedAt = observedAt.UTC()

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Client{}, nil, nil, ErrNameRequired
	}
	if !clientNameRegex.MatchString(name) {
		return Client{}, nil, nil, ErrNameInvalid
	}
	// Client names are the Telemt username: two managed clients sharing one
	// collapse into a single Telemt user on any common node. Enforce global
	// uniqueness among living clients (excludeID = "" on create).
	if s.NameTaken(name, "") {
		return Client{}, nil, nil, ErrNameTaken
	}

	userADTag, err := resolveUserADTagForMutation(input, "")
	if err != nil {
		return Client{}, nil, nil, err
	}

	secret := strings.TrimSpace(input.Secret)
	if secret != "" {
		if !IsValidHexSecret(secret) {
			return Client{}, nil, nil, fmt.Errorf("invalid secret format: must be 32 hex characters")
		}
	} else {
		secret, err = RandomHexString(16)
		if err != nil {
			return Client{}, nil, nil, err
		}
	}

	subscriptionToken, err := GenerateSubscriptionToken()
	if err != nil {
		return Client{}, nil, nil, fmt.Errorf("generate subscription token: %w", err)
	}

	expirationRFC3339, err := NormalizeExpiration(input.ExpirationRFC3339)
	if err != nil {
		return Client{}, nil, nil, err
	}

	if err := validateClientLimits(input.MaxTCPConns, input.MaxUniqueIPs, input.DataQuotaBytes); err != nil {
		return Client{}, nil, nil, err
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	client := Client{
		ID:                ClientID(s.NextClientID()),
		Name:              name,
		Secret:            secret,
		UserADTag:         userADTag,
		Enabled:           enabled,
		MaxTCPConns:       input.MaxTCPConns,
		MaxUniqueIPs:      input.MaxUniqueIPs,
		DataQuotaBytes:    input.DataQuotaBytes,
		ExpirationRFC3339: expirationRFC3339,
		SubscriptionToken: subscriptionToken,
		CreatedAt:         observedAt,
		UpdatedAt:         observedAt,
	}

	assignments := s.buildClientAssignments(client.ID, input, observedAt)
	targetAgentIDs := s.ResolveTargetAgentIDs(assignments, s.deps.Topology())
	if len(targetAgentIDs) == 0 {
		return Client{}, nil, nil, ErrTargetsRequired
	}

	deployments := BuildDeployments(nil, client.ID, targetAgentIDs, string(jobs.ActionClientCreate), string(jobs.ActionClientDelete), observedAt)
	// Persist client state before enqueuing the job so a failure in
	// persistence does not leave a dispatched job referencing unknown state.
	if err := s.saveStateAndPublish(ctx, client, assignments, deployments); err != nil {
		return Client{}, nil, nil, err
	}
	if _, err := s.EnqueueClientJob(ctx, actorID, jobs.ActionClientCreate, client, targetAgentIDs, observedAt); err != nil {
		return Client{}, nil, nil, err
	}

	return client, assignments, deployments, nil
}

// Update merges the mutation input into the stored client, persists it,
// and dispatches update (and delete-for-dropped-target) jobs.
func (s *Service) Update(ctx context.Context, clientID, actorID string, input MutationInput, observedAt time.Time) (Client, []Assignment, []Deployment, error) {
	observedAt = observedAt.UTC()

	currentClient, _, currentDeployments, err := s.detailSnapshot(clientID)
	if err != nil {
		return Client{}, nil, nil, err
	}
	if currentClient.DeletedAt != nil {
		return Client{}, nil, nil, storage.ErrNotFound
	}

	// Audit F2: Telemt has no rename operation (PatchUserRequest has no
	// username field), so the client name is immutable after create. Reject
	// a name change up-front, before any state is mutated or persisted.
	// The name-uniqueness check that used to run here is moot: the name
	// cannot change, and it was already validated unique at create time.
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Client{}, nil, nil, ErrNameRequired
	}
	if name != currentClient.Name {
		return Client{}, nil, nil, ErrNameImmutable
	}

	if err := applyClientMutationFields(&currentClient, input, observedAt); err != nil {
		return Client{}, nil, nil, err
	}

	assignments := s.buildClientAssignments(ClientID(clientID), input, observedAt)
	targetAgentIDs := s.ResolveTargetAgentIDs(assignments, s.deps.Topology())
	deployments := BuildDeployments(currentDeployments, ClientID(clientID), targetAgentIDs, string(jobs.ActionClientUpdate), string(jobs.ActionClientDelete), observedAt)

	// Persist client state before enqueuing jobs so a failure in
	// persistence does not leave dispatched jobs referencing stale state.
	if err := s.saveStateAndPublish(ctx, currentClient, assignments, deployments); err != nil {
		return Client{}, nil, nil, err
	}

	if err := s.DispatchClientUpdateJobs(ctx, actorID, currentClient, currentDeployments, targetAgentIDs, observedAt); err != nil {
		return Client{}, nil, nil, err
	}

	return currentClient, assignments, deployments, nil
}

// validateClientLimits rejects negative numeric limits before they are
// persisted and pushed to Telemt. Zero means "no limit" (a deliberate
// clear); negative values are nonsensical and were previously accepted
// verbatim into the DB and rollout payload.
func validateClientLimits(maxTCPConns, maxUniqueIPs int, dataQuotaBytes int64) error {
	if maxTCPConns < 0 || maxUniqueIPs < 0 || dataQuotaBytes < 0 {
		return ErrLimitNegative
	}
	return nil
}

// applyClientMutationFields validates the mutation input and merges it
// into currentClient in-place. The Name is NOT touched — it is immutable
// after create (audit F2: Telemt has no rename operation); Update rejects
// a changed name before calling this.
func applyClientMutationFields(currentClient *Client, input MutationInput, observedAt time.Time) error {
	userADTag, err := resolveUserADTagForMutation(input, currentClient.UserADTag)
	if err != nil {
		return err
	}

	expirationRFC3339, err := NormalizeExpiration(input.ExpirationRFC3339)
	if err != nil {
		return err
	}

	if err := validateClientLimits(input.MaxTCPConns, input.MaxUniqueIPs, input.DataQuotaBytes); err != nil {
		return err
	}

	enabled := currentClient.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	currentClient.UserADTag = userADTag
	currentClient.Enabled = enabled
	currentClient.MaxTCPConns = input.MaxTCPConns
	currentClient.MaxUniqueIPs = input.MaxUniqueIPs
	currentClient.DataQuotaBytes = input.DataQuotaBytes
	currentClient.ExpirationRFC3339 = expirationRFC3339
	currentClient.UpdatedAt = observedAt
	return nil
}

// Redeploy re-queues the create job for every target agent on the client.
// Used to recover a client whose initial rollout partially or fully failed
// — the panel still has the record, but one or more Telemt nodes rejected
// the apply (bad ad tag, network blip, etc.). Re-running the flow with the
// current stored state is the operator-facing equivalent of "retry
// deployment".
func (s *Service) Redeploy(ctx context.Context, clientID, actorID string, observedAt time.Time) (Client, []Assignment, []Deployment, error) {
	observedAt = observedAt.UTC()

	currentClient, assignments, deployments, err := s.detailSnapshot(clientID)
	if err != nil {
		return Client{}, nil, nil, err
	}
	if currentClient.DeletedAt != nil {
		return Client{}, nil, nil, storage.ErrNotFound
	}

	targetAgentIDs := s.ResolveTargetAgentIDs(assignments, s.deps.Topology())
	if len(targetAgentIDs) == 0 {
		// No targets at all — nothing to redeploy. Return current state
		// so the caller surfaces "no-op" gracefully rather than looking
		// like a silent success.
		return currentClient, assignments, deployments, nil
	}

	deployments = BuildDeployments(deployments, ClientID(clientID), targetAgentIDs, string(jobs.ActionClientCreate), string(jobs.ActionClientDelete), observedAt)
	if err := s.saveStateAndPublish(ctx, currentClient, assignments, deployments); err != nil {
		return Client{}, nil, nil, err
	}
	if _, err := s.EnqueueClientJob(ctx, actorID, jobs.ActionClientCreate, currentClient, targetAgentIDs, observedAt); err != nil {
		return Client{}, nil, nil, err
	}
	return currentClient, assignments, deployments, nil
}

// RotateSecret mints a fresh secret, persists it before enqueuing the
// rotation job, and returns the updated state.
func (s *Service) RotateSecret(ctx context.Context, clientID, actorID string, observedAt time.Time) (Client, []Assignment, []Deployment, error) {
	observedAt = observedAt.UTC()

	currentClient, assignments, deployments, err := s.detailSnapshot(clientID)
	if err != nil {
		return Client{}, nil, nil, err
	}
	if currentClient.DeletedAt != nil {
		return Client{}, nil, nil, storage.ErrNotFound
	}

	secret, err := RandomHexString(16)
	if err != nil {
		return Client{}, nil, nil, err
	}
	currentClient.Secret = secret
	currentClient.UpdatedAt = observedAt

	targetAgentIDs := s.ResolveTargetAgentIDs(assignments, s.deps.Topology())
	deployments = BuildDeployments(deployments, ClientID(clientID), targetAgentIDs, string(jobs.ActionClientRotateSecret), string(jobs.ActionClientDelete), observedAt)
	// Persist the new secret before enqueuing the rotation job so a
	// persistence failure does not leave a dispatched job with a secret
	// the control-plane never recorded.
	if err := s.saveStateAndPublish(ctx, currentClient, assignments, deployments); err != nil {
		return Client{}, nil, nil, err
	}
	if len(targetAgentIDs) > 0 {
		if _, err := s.EnqueueClientJob(ctx, actorID, jobs.ActionClientRotateSecret, currentClient, targetAgentIDs, observedAt); err != nil {
			return Client{}, nil, nil, err
		}
	}

	return currentClient, assignments, deployments, nil
}

// RotateSubscriptionToken assigns a fresh subscription token to the client and
// persists the change. The old token becomes invalid immediately — any
// in-flight /sub/<old-token> requests will see ErrNotFound after this returns.
//
// Unlike RotateSecret no agent job is enqueued: the subscription token
// is a panel-side handle used only for the /sub page; agents never receive it.
// Mirrors the same load→mutate→persist shape as RotateSecret.
func (s *Service) RotateSubscriptionToken(ctx context.Context, clientID, actorID string, observedAt time.Time) (Client, []Assignment, []Deployment, error) {
	observedAt = observedAt.UTC()

	currentClient, assignments, deployments, err := s.detailSnapshot(clientID)
	if err != nil {
		return Client{}, nil, nil, err
	}
	if currentClient.DeletedAt != nil {
		return Client{}, nil, nil, storage.ErrNotFound
	}

	newToken, err := GenerateSubscriptionToken()
	if err != nil {
		return Client{}, nil, nil, fmt.Errorf("generate subscription token: %w", err)
	}
	currentClient.SubscriptionToken = newToken
	currentClient.UpdatedAt = observedAt

	if err := s.saveStateAndPublish(ctx, currentClient, assignments, deployments); err != nil {
		return Client{}, nil, nil, err
	}

	return currentClient, assignments, deployments, nil
}

// ResetQuota enqueues a client.reset_quota job for one or more agents
// hosting the given client. When targetAgentID is empty, the job fans out
// to every currently-assigned agent; otherwise it targets only the one
// specified agent — caller must have validated that the agent currently
// hosts the client.
//
// Unlike rotate-secret / update / delete this is a counter-reset, not
// a config mutation, so the panel does NOT persist a new client state
// before enqueuing. A failed job (e.g. Telemt unreachable) does not
// leave the panel in an inconsistent state — the operator just sees
// the failure in the Jobs view and can re-trigger.
func (s *Service) ResetQuota(ctx context.Context, clientID, targetAgentID, actorID string, observedAt time.Time) (Client, []Assignment, []Deployment, jobs.Job, error) {
	observedAt = observedAt.UTC()

	currentClient, assignments, deployments, err := s.detailSnapshot(clientID)
	if err != nil {
		return Client{}, nil, nil, jobs.Job{}, err
	}
	if currentClient.DeletedAt != nil {
		return Client{}, nil, nil, jobs.Job{}, storage.ErrNotFound
	}

	deploymentAgents := DeploymentAgentIDs(deployments)
	var targetAgentIDs []string
	if targetAgentID == "" {
		targetAgentIDs = deploymentAgents
	} else {
		// Validate that the requested agent is currently a deployment
		// target for this client — operators can't reset on agents the
		// client was never deployed to.
		matched := false
		for _, agentID := range deploymentAgents {
			if agentID == targetAgentID {
				matched = true
				break
			}
		}
		if !matched {
			return Client{}, nil, nil, jobs.Job{}, storage.ErrNotFound
		}
		targetAgentIDs = []string{targetAgentID}
	}

	if len(targetAgentIDs) == 0 {
		// Nothing to do — no deployments. Return an empty Job so the
		// caller can render "no agents to reset" without erroring.
		return currentClient, assignments, deployments, jobs.Job{}, nil
	}

	job, err := s.EnqueueClientResetQuotaJob(ctx, actorID, currentClient, targetAgentIDs, observedAt)
	if err != nil {
		return Client{}, nil, nil, jobs.Job{}, err
	}
	return currentClient, assignments, deployments, job, nil
}

// DeleteFlow tombstones the client (Enabled=false, DeletedAt set),
// persists the tombstone before dispatching the delete job, and enqueues
// the delete job for every target agent. Named DeleteFlow to avoid
// colliding with Service.Delete (the repo-level eviction).
func (s *Service) DeleteFlow(ctx context.Context, clientID, actorID string, observedAt time.Time) error {
	observedAt = observedAt.UTC()

	currentClient, assignments, deployments, err := s.detailSnapshot(clientID)
	if err != nil {
		return err
	}
	if currentClient.DeletedAt != nil {
		return storage.ErrNotFound
	}

	currentClient.Enabled = false
	currentClient.UpdatedAt = observedAt
	currentClient.DeletedAt = &observedAt

	targetAgentIDs := s.ResolveTargetAgentIDs(assignments, s.deps.Topology())
	if len(targetAgentIDs) == 0 {
		targetAgentIDs = DeploymentAgentIDs(deployments)
	}
	deployments = BuildDeployments(deployments, ClientID(clientID), targetAgentIDs, string(jobs.ActionClientDelete), string(jobs.ActionClientDelete), observedAt)

	// Persist the tombstone before dispatching the delete job so a persistence
	// failure does not leave the agent with a removed client while the DB
	// record still shows DeletedAt=nil (ghost state, see P2-LOG-01 / M-C1).
	if err := s.saveStateAndPublish(ctx, currentClient, assignments, deployments); err != nil {
		return err
	}

	if len(targetAgentIDs) > 0 {
		if _, err := s.EnqueueClientJob(ctx, actorID, jobs.ActionClientDelete, currentClient, targetAgentIDs, observedAt); err != nil {
			return err
		}
	}

	return nil
}

// saveStateAndPublish persists the client state (Repository when wired,
// in-memory mirror otherwise) and publishes a clients.updated event. This is
// the single owner of client-state writes; the server package's tests drive
// it through the exported SaveState + PublishClientsUpdated pair (R8a Task
// 11 removed the server-side replaceClientStateWithContext duplicate).
func (s *Service) saveStateAndPublish(ctx context.Context, client Client, assignments []Assignment, deployments []Deployment) error {
	if s.HasRepo() {
		// NewService path: SaveState atomically writes to the Repository and
		// updates the Service mirror (the single owner of client state).
		if err := s.SaveState(ctx, client, assignments, deployments); err != nil {
			return err
		}
	} else {
		// No-repo fallback (test doubles / pre-migrate stores): update the
		// in-memory mirror directly.
		s.MirrorReplaceInMemory(client, assignments, deployments)
	}
	s.deps.PublishClientsUpdated(client.ID)
	return nil
}

// buildClientAssignments materialises the assignment rows for a mutation
// input, minting a fresh sequence ID per row.
func (s *Service) buildClientAssignments(clientID ClientID, input MutationInput, observedAt time.Time) []Assignment {
	assignments := make([]Assignment, 0, len(input.FleetGroupIDs)+len(input.AgentIDs))
	for _, fleetGroupID := range NormalizedIDs(input.FleetGroupIDs) {
		assignments = append(assignments, Assignment{
			ID:           AssignmentID(s.NextAssignmentID()),
			ClientID:     clientID,
			TargetType:   TargetTypeFleetGroup,
			FleetGroupID: FleetGroupID(fleetGroupID),
			CreatedAt:    observedAt,
		})
	}
	for _, agentID := range NormalizedIDs(input.AgentIDs) {
		assignments = append(assignments, Assignment{
			ID:         AssignmentID(s.NextAssignmentID()),
			ClientID:   clientID,
			TargetType: TargetTypeAgent,
			AgentID:    agentID,
			CreatedAt:  observedAt,
		})
	}

	return assignments
}

// resolveUserADTagForMutation honours the tri-state
// MutationInput.UserADTagAuto flag:
//   - nil or *true  → legacy auto-gen / fallback behaviour.
//   - *false        → operator explicitly opted out of auto-gen;
//     empty stored as empty, non-empty must be valid hex.
//
// All branches surface ErrUserADTag on invalid input.
func resolveUserADTagForMutation(input MutationInput, fallback string) (string, error) {
	if input.UserADTagAuto != nil && !*input.UserADTagAuto {
		return ResolveUserADTagExplicit(input.UserADTag)
	}
	return ResolveUserADTag(input.UserADTag, fallback)
}

// detailSnapshot sources detail data from the in-memory mirror (the single
// owner of client/assignment/deployment state). The mirror is kept current
// on every write path (SaveState / PersistDeployment / UpsertUsage*), so the
// projected shape and sort order match the prior server-map read. The
// server-side counterpart is clientDetailSnapshot.
func (s *Service) detailSnapshot(clientID string) (Client, []Assignment, []Deployment, error) {
	mirror := s.MirrorSnapshot()

	cid := ClientID(clientID)
	client, ok := mirror.Clients[cid]
	if !ok {
		return Client{}, nil, nil, storage.ErrNotFound
	}

	assignments := append([]Assignment(nil), mirror.Assignments[cid]...)
	sort.Slice(assignments, func(left, right int) bool {
		if assignments[left].CreatedAt.Equal(assignments[right].CreatedAt) {
			return assignments[left].ID < assignments[right].ID
		}
		return assignments[left].CreatedAt.Before(assignments[right].CreatedAt)
	})

	deploymentsMap := mirror.Deployments[cid]
	deployments := make([]Deployment, 0, len(deploymentsMap))
	for _, deployment := range deploymentsMap {
		deployments = append(deployments, deployment)
	}
	sort.Slice(deployments, func(left, right int) bool {
		return deployments[left].AgentID < deployments[right].AgentID
	})

	return client, assignments, deployments, nil
}
