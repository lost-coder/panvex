package server

import (
	"context"
	"time"
)

// Q4.U-S-23: mid-stream revocation watcher. The Connect-time check only
// catches a cert that was already revoked when the stream opened. A
// long-lived stream still has to honour an operator who revokes the agent
// later — without this ticker, the agent could keep running for the cert's
// full validity window. 30s is fast enough that an operator-initiated
// revocation hits within a dashboard refresh, slow enough not to add
// noticeable RPS.
func (s *Server) startRevocationWatcher(ctx context.Context, cancel context.CancelFunc, agentID, presentedSerial string) {
	go func() {
		defer s.recoverAgentStreamGoroutine(agentID, "revocation-watcher", cancel)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.shouldTerminateForRevocation(ctx, agentID, presentedSerial) {
					cancel()
					return
				}
			}
		}
	}()
}

// shouldTerminateForRevocation returns true when either the in-memory
// revoked set has the agent or the persisted cert pin no longer matches the
// presented serial.
func (s *Server) shouldTerminateForRevocation(ctx context.Context, agentID, presentedSerial string) bool {
	s.mu.RLock()
	_, isRevoked := s.revokedAgentIDs[agentID]
	s.mu.RUnlock()
	if isRevoked {
		s.logger.InfoContext(ctx, "mid-stream revocation triggered, terminating agent stream", "agent_id", agentID)
		return true
	}
	if s.store == nil {
		return false
	}
	expected, err := s.store.GetAgentCertSerial(ctx, agentID)
	if err != nil {
		// Fail closed: a transient lookup failure must not silently
		// strip the only defense against harvested-cert replay. Tearing
		// the stream down forces the agent to reconnect, which retries
		// pin verification from scratch under authorizeAgentConnect.
		// On context cancel (graceful shutdown) the caller is already
		// going away, so the extra termination is a no-op.
		if ctx.Err() != nil {
			return false
		}
		s.logger.WarnContext(ctx, "mid-stream cert pin lookup failed, terminating",
			"agent_id", agentID, "error", err)
		return true
	}
	if expected != "" && expected != presentedSerial {
		s.logger.InfoContext(ctx, "mid-stream cert pin mismatch, terminating", "agent_id", agentID)
		return true
	}
	return false
}
