package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// DispatchClientUpdateJobs queues an update job for the current target
// agents and a delete job for any agents the client no longer targets.
func (s *Service) DispatchClientUpdateJobs(ctx context.Context, actorID string, currentClient Client, currentDeployments []Deployment, targetAgentIDs []string, observedAt time.Time) error {
	if len(targetAgentIDs) > 0 {
		if _, err := s.EnqueueClientJob(ctx, actorID, jobs.ActionClientUpdate, currentClient, targetAgentIDs, observedAt); err != nil {
			return err
		}
	}

	removedAgentIDs := RemovedTargetAgentIDs(currentDeployments, targetAgentIDs)
	if len(removedAgentIDs) > 0 {
		if _, err := s.EnqueueClientJob(ctx, actorID, jobs.ActionClientDelete, currentClient, removedAgentIDs, observedAt); err != nil {
			return err
		}
	}
	return nil
}

// EnqueueClientResetQuotaJob enqueues a client.reset_quota job carrying
// just the (client_id, name) needed by the agent — full ManagedClient
// would be wasteful and would also leak Secret (which we never want to
// resend over the wire for what is purely a counter-reset operation).
//
// targetAgentIDs may be a single-agent list (targeted reset) or every
// agent currently hosting the client (fan-out). Each Telemt instance
// reports back independently via JobResult.ResultJSON, so a mixed
// fleet with some Telemt < 3.4.6 surfaces the per-agent
// unsupported_telemt flag without affecting the rest.
func (s *Service) EnqueueClientResetQuotaJob(ctx context.Context, actorID string, client Client, targetAgentIDs []string, observedAt time.Time) (jobs.Job, error) {
	payloadJSON, err := json.Marshal(ClientResetQuotaJobPayload{
		ClientID: string(client.ID),
		Name:     client.Name,
	})
	if err != nil {
		return jobs.Job{}, err
	}
	return s.enqueueClientJobAndPublish(ctx, actorID, jobs.ActionClientResetQuota, client.ID, targetAgentIDs, string(payloadJSON), observedAt)
}

// enqueueClientJobAndPublish enqueues a client-scoped job with the shared
// idempotency-key shape (action:clientID:observedAtNanos), then notifies the
// target agents and publishes the job-created event. Both client job dispatch
// paths funnel through here so the post-enqueue notify/publish sequence stays
// single-sourced — a future change (extra metric, second notify) lands once.
func (s *Service) enqueueClientJobAndPublish(ctx context.Context, actorID string, action jobs.Action, clientID ClientID, targetAgentIDs []string, payloadJSON string, observedAt time.Time) (jobs.Job, error) {
	readOnlyAgents := s.deps.ReadOnlyAgents(targetAgentIDs)

	job, err := s.jobQueue.Enqueue(ctx, jobs.CreateJobInput{
		Action:         action,
		TargetAgentIDs: targetAgentIDs,
		TTL:            s.deps.ClientJobTTL(),
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", action, clientID, observedAt.UnixNano()),
		ActorID:        actorID,
		ReadOnlyAgents: readOnlyAgents,
		PayloadJSON:    payloadJSON,
	}, observedAt)
	if err != nil {
		return jobs.Job{}, err
	}
	s.deps.NotifyAgentSessions(job.TargetAgentIDs)
	s.deps.PublishJobCreated(job)
	return job, nil
}

// ClientResetQuotaJobPayload mirrors the same-named struct on the
// agent side (internal/agent/runtime/agent.go). Keep the JSON tags
// in lockstep — any drift here breaks the agent's payload decode and
// the job will fail with "invalid reset_quota payload".
type ClientResetQuotaJobPayload struct {
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
}

// ClientJobPayload mirrors the agent's clientJobPayload
// (internal/agent/runtime/agent.go). Keep the JSON tags in lockstep — any
// drift here breaks the agent's payload decode.
type ClientJobPayload struct {
	ClientID          string `json:"client_id"`
	Name              string `json:"name"`
	Secret            string `json:"secret"`
	UserADTag         string `json:"user_ad_tag"`
	Enabled           bool   `json:"enabled"`
	MaxTCPConns       int    `json:"max_tcp_conns"`
	MaxUniqueIPs      int    `json:"max_unique_ips"`
	DataQuotaBytes    int64  `json:"data_quota_bytes"`
	ExpirationRFC3339 string `json:"expiration_rfc3339"`

	// NameOwnerByAgent is set on client.delete ONLY: agentID -> the ID of the
	// client that, in panel state, currently owns Name on THAT agent. Absent
	// for every agent where nobody but the client being deleted holds the name
	// — which is the overwhelmingly common case, so the map is normally
	// omitted entirely.
	//
	// It exists because the agent cannot answer "did this name change hands?"
	// on its own after a restart: its clientID->name registry is cold, and the
	// only other local evidence — the secret Telemt holds for the user —
	// mismatches for TWO indistinguishable reasons (someone took the name, OR
	// a rotate for this same client never reached the node and was superseded
	// by this delete). The panel knows which, so it says so.
	//
	// PER-AGENT, not global: one delete job fans out to every agent that still
	// hosts the client with ONE shared payload, and the Telemt user "alice" on
	// agent-2 belongs to whichever client is deployed to agent-2. A name
	// re-used on agent-1 must never suppress the delete on agent-2 — that
	// would strand a live user the panel reports as deleted. Hence a map
	// rather than a bool or a bare owner ID.
	NameOwnerByAgent map[string]string `json:"name_owner_by_agent,omitempty"`
}

func (s *Service) EnqueueClientJob(ctx context.Context, actorID string, action jobs.Action, client Client, targetAgentIDs []string, observedAt time.Time) (jobs.Job, error) {
	payload := ClientJobPayload{
		ClientID:          string(client.ID),
		Name:              client.Name,
		Secret:            client.Secret,
		UserADTag:         client.UserADTag,
		Enabled:           client.Enabled,
		MaxTCPConns:       client.MaxTCPConns,
		MaxUniqueIPs:      client.MaxUniqueIPs,
		DataQuotaBytes:    client.DataQuotaBytes,
		ExpirationRFC3339: client.ExpirationRFC3339,
	}
	if action == jobs.ActionClientDelete {
		// Computed HERE, at every (re-)enqueue, rather than by the callers:
		// the reconciler re-enqueues an expired delete with a fresh CreatedAt
		// precisely when the name may have been re-used since, so a value
		// carried over from the first enqueue would be exactly the stale
		// answer that reopens the hole.
		payload.NameOwnerByAgent = s.nameOwnersByAgent(client.Name, client.ID, targetAgentIDs)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return jobs.Job{}, err
	}
	return s.enqueueClientJobAndPublish(ctx, actorID, action, client.ID, targetAgentIDs, string(payloadJSON), observedAt)
}

// nameOwnersByAgent answers, for each of the given agents, which OTHER living
// client currently owns name on that agent — i.e. which client's Telemt user
// would be destroyed if an agent there deleted the user by name alone. Returns
// nil when no agent has such an owner, so the payload key is omitted.
//
// A client counts as an owner on an agent when it is (a) not excludeID, (b)
// living — a tombstoned client's user is itself being torn down, so it never
// protects the name — and (c) has a deployment row for that agent whose
// desired operation is not itself a delete. Excluding delete-desired
// deployments only ever makes the agent MORE willing to delete, and both
// clients want the user gone in that case; the opposite bias (over-reporting
// owners) is the one that strands users.
//
// Walks mirrorNamesIndex[name], so it is O(clients-with-that-name × agents) —
// and the panel enforces global name uniqueness among living clients, so in
// practice that is at most one candidate.
func (s *Service) nameOwnersByAgent(name string, excludeID ClientID, agentIDs []string) map[string]string {
	if name == "" || len(agentIDs) == 0 {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var owners map[string]string
	for id := range s.mirrorNamesIndex[name] {
		if id == excludeID {
			continue
		}
		c, ok := s.mirrorClients[id]
		if !ok || c.DeletedAt != nil {
			continue
		}
		byAgent := s.mirrorDeployments[id]
		for _, agentID := range agentIDs {
			deployment, ok := byAgent[agentID]
			if !ok || deployment.DesiredOperation == string(jobs.ActionClientDelete) {
				continue
			}
			if owners == nil {
				owners = make(map[string]string, 1)
			}
			owners[agentID] = string(id)
		}
	}
	return owners
}
