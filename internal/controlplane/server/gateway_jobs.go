package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func (s *Server) dispatchPendingJobs(ctx context.Context, sess agenttransport.AgentSession, agentID string) error {
	pendingJobs := s.pendingJobsForAgent(ctx, agentID)
	if len(pendingJobs) == 0 {
		return nil
	}

	hasMore := len(pendingJobs) > jobDispatchBatchSize
	if hasMore {
		pendingJobs = pendingJobs[:jobDispatchBatchSize]
	}

	for _, job := range pendingJobs {
		s.logger.DebugContext(ctx, "job dispatched to agent", "agent_id", agentID, "job_id", job.ID, "action", string(job.Action))
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
		s.markJobDelivered(ctx, agentID, job.ID)
	}

	if hasMore {
		s.notifyAgentSession(agentID)
	}

	return nil
}

func (s *Server) pendingJobsForAgent(ctx context.Context, agentID string) []jobs.Job {
	return s.jobs.PendingForAgent(ctx, agentID, jobDispatchRetryAfter)
}

func (s *Server) markJobDelivered(ctx context.Context, agentID string, jobID string) {
	s.jobs.MarkDelivered(ctx, agentID, jobID, s.now())
}

// Test-only convenience wrappers. Production code drives these flows
// through processPriorityAgentMessageAsync with the connection ctx.
func (s *Server) recordJobAcknowledged(ctx context.Context, agentID string, jobID string, observedAt time.Time) {
	s.recordJobAcknowledgedState(ctx, agentID, jobID, observedAt)
	s.appendAuditWithContext(ctx, agentID, auditJobsAcknowledged, jobID, map[string]any{})
}

func (s *Server) recordJobResult(ctx context.Context, agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
	s.recordJobResultState(ctx, agentID, jobID, success, message, resultJSON, observedAt)
	s.recordClientJobResultWithContext(ctx, agentID, jobID, success, message, resultJSON, observedAt)
	s.appendAuditWithContext(ctx, agentID, auditJobsResult, jobID, map[string]any{
		"success": success,
		"message": message,
	})
}

func (s *Server) recordJobResultState(ctx context.Context, agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
	if !s.jobs.RecordResult(ctx, agentID, jobID, success, message, resultJSON, observedAt) {
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

func (s *Server) recordJobAcknowledgedState(ctx context.Context, agentID string, jobID string, observedAt time.Time) {
	s.jobs.MarkAcknowledged(ctx, agentID, jobID, observedAt)
}
