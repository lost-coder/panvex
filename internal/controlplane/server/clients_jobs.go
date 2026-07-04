package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// dispatchClientUpdateJobs queues an update job for the current target
// agents and a delete job for any agents the client no longer targets.
func (s *Server) dispatchClientUpdateJobs(ctx context.Context, actorID string, currentClient managedClient, previousName string, currentDeployments []managedClientDeployment, targetAgentIDs []string, observedAt time.Time) error {
	if len(targetAgentIDs) > 0 {
		if _, err := s.enqueueClientJob(ctx, actorID, jobs.ActionClientUpdate, currentClient, previousName, targetAgentIDs, observedAt); err != nil {
			return err
		}
	}

	removedAgentIDs := removedClientTargetAgentIDs(currentDeployments, targetAgentIDs)
	if len(removedAgentIDs) > 0 {
		if _, err := s.enqueueClientJob(ctx, actorID, jobs.ActionClientDelete, currentClient, "", removedAgentIDs, observedAt); err != nil {
			return err
		}
	}
	return nil
}

// enqueueClientResetQuotaJob enqueues a client.reset_quota job carrying
// just the (client_id, name) needed by the agent — full ManagedClient
// would be wasteful and would also leak Secret (which we never want to
// resend over the wire for what is purely a counter-reset operation).
//
// targetAgentIDs may be a single-agent list (targeted reset) or every
// agent currently hosting the client (fan-out). Each Telemt instance
// reports back independently via JobResult.ResultJSON, so a mixed
// fleet with some Telemt < 3.4.6 surfaces the per-agent
// unsupported_telemt flag without affecting the rest.
func (s *Server) enqueueClientResetQuotaJob(ctx context.Context, actorID string, client managedClient, targetAgentIDs []string, observedAt time.Time) (jobs.Job, error) {
	payloadJSON, err := json.Marshal(clientResetQuotaJobPayload{
		ClientID: string(client.ID),
		Name:     client.Name,
	})
	if err != nil {
		return jobs.Job{}, err
	}

	readOnlyAgents := make(map[string]bool, len(targetAgentIDs))
	for _, agentID := range targetAgentIDs {
		if agent, ok := s.live.Get(agentID); ok {
			readOnlyAgents[agentID] = agent.ReadOnly
		}
	}

	job, err := s.jobs.Enqueue(ctx, jobs.CreateJobInput{
		Action:         jobs.ActionClientResetQuota,
		TargetAgentIDs: targetAgentIDs,
		TTL:            s.effectiveClientJobTTL(),
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", jobs.ActionClientResetQuota, client.ID, observedAt.UnixNano()),
		ActorID:        actorID,
		ReadOnlyAgents: readOnlyAgents,
		PayloadJSON:    string(payloadJSON),
	}, observedAt)
	if err != nil {
		return jobs.Job{}, err
	}
	s.notifyAgentSessions(job.TargetAgentIDs)
	s.publishJobCreated(job)
	return job, nil
}

// clientResetQuotaJobPayload mirrors the same-named struct on the
// agent side (internal/agent/runtime/agent.go). Keep the JSON tags
// in lockstep — any drift here breaks the agent's payload decode and
// the job will fail with "invalid reset_quota payload".
type clientResetQuotaJobPayload struct {
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
}

func (s *Server) enqueueClientJob(ctx context.Context, actorID string, action jobs.Action, client managedClient, previousName string, targetAgentIDs []string, observedAt time.Time) (jobs.Job, error) {
	payloadJSON, err := json.Marshal(clientJobPayload{
		ClientID:          string(client.ID),
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
	for _, agentID := range targetAgentIDs {
		if agent, ok := s.live.Get(agentID); ok {
			readOnlyAgents[agentID] = agent.ReadOnly
		}
	}

	job, err := s.jobs.Enqueue(ctx, jobs.CreateJobInput{
		Action:         action,
		TargetAgentIDs: targetAgentIDs,
		TTL:            s.effectiveClientJobTTL(),
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", action, client.ID, observedAt.UnixNano()),
		ActorID:        actorID,
		ReadOnlyAgents: readOnlyAgents,
		PayloadJSON:    string(payloadJSON),
	}, observedAt)
	if err != nil {
		return jobs.Job{}, err
	}
	s.notifyAgentSessions(job.TargetAgentIDs)
	s.publishJobCreated(job)

	return job, nil
}
