package server

import (
	"context"
	"time"
)

// recordJobResultForTest replays the full "agent reported a job result"
// side-effect chain (jobs state + client deployment update + audit) that the
// pre-P8.2d Server.recordJobResult convenience wrapper provided. That wrapper
// moved into the gateway package with the stream code, so server-package
// tests keep this local helper to drive the flow without a live stream.
func recordJobResultForTest(s *Server, ctx context.Context, agentID, jobID string, success bool, message, resultJSON string, observedAt time.Time) {
	s.jobs.RecordResult(ctx, agentID, jobID, success, message, resultJSON, observedAt)
	s.recordClientJobResultWithContext(ctx, agentID, jobID, success, message, resultJSON, observedAt)
	s.appendAuditWithContext(ctx, agentID, "jobs.result", jobID, map[string]any{
		"success": success,
		"message": message,
	})
}

// markJobDeliveredForTest mirrors the former Server.markJobDelivered wrapper
// (now a gateway method) for server-package tests.
func markJobDeliveredForTest(s *Server, ctx context.Context, agentID, jobID string) {
	s.jobs.MarkDelivered(ctx, agentID, jobID, s.now())
}
