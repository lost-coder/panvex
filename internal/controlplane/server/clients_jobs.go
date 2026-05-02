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

func (s *Server) enqueueClientJob(ctx context.Context, actorID string, action jobs.Action, client managedClient, previousName string, targetAgentIDs []string, observedAt time.Time) (jobs.Job, error) {
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

	job, err := s.jobs.Enqueue(ctx, jobs.CreateJobInput{
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
