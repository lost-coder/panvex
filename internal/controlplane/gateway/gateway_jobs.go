package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func (g *Gateway) dispatchPendingJobs(ctx context.Context, sess agenttransport.AgentSession, agentID string) error {
	pendingJobs := g.pendingJobsForAgent(ctx, agentID)
	if len(pendingJobs) == 0 {
		return nil
	}

	hasMore := len(pendingJobs) > jobDispatchBatchSize
	if hasMore {
		pendingJobs = pendingJobs[:jobDispatchBatchSize]
	}

	for _, job := range pendingJobs {
		g.logger.DebugContext(ctx, "job dispatched to agent", "agent_id", agentID, "job_id", job.ID, "action", string(job.Action))
		if err := sess.Send(&gatewayrpc.ConnectServerMessage{
			Body: &gatewayrpc.ConnectServerMessage_Job{
				Job: &gatewayrpc.JobCommand{
					Id:             job.ID,
					Action:         string(job.Action),
					IdempotencyKey: job.IdempotencyKey,
					TargetAgentIds: job.TargetAgentIDs,
					PayloadJson:    job.PayloadJSON,
				},
			},
		}); err != nil {
			return err
		}
		g.markJobDelivered(ctx, agentID, job.ID)
	}

	if hasMore {
		g.deps.NotifyAgentSession(agentID)
	}

	return nil
}

func (g *Gateway) pendingJobsForAgent(ctx context.Context, agentID string) []jobs.Job {
	return g.jobs.PendingForAgent(ctx, agentID, jobDispatchRetryAfter)
}

func (g *Gateway) markJobDelivered(ctx context.Context, agentID string, jobID string) {
	g.jobs.MarkDelivered(ctx, agentID, jobID, g.now())
}

func (g *Gateway) recordJobResultState(ctx context.Context, agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
	if !g.jobs.RecordResult(ctx, agentID, jobID, success, message, resultJSON, observedAt) {
		// P2-LOG-05: the job was evicted (terminal-key TTL, acknowledged
		// expiry worker, or a late result arriving long after the agent's
		// idempotency window) before this result reached the CP. Warn and
		// ignore — the agent's own 2h idempotency cache ensures replay
		// safety, so dropping the late result here is the correct
		// idempotent safety net.
		slog.Warn("job result for unknown or evicted job",
			"agent_id", agentID,
			"job_id", jobID,
			"success", success)
	}
}

func (g *Gateway) recordJobAcknowledgedState(ctx context.Context, agentID string, jobID string, observedAt time.Time) {
	g.jobs.MarkAcknowledged(ctx, agentID, jobID, observedAt)
}
