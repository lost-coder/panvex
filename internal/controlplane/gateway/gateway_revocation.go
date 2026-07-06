package gateway

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
// noticeable RPS. The revocation decision (in-memory set + persisted cert
// pin) lives in server behind Deps.ShouldTerminateForRevocation.
func (g *Gateway) startRevocationWatcher(ctx context.Context, cancel context.CancelFunc, agentID, presentedSerial string) {
	go func() {
		defer g.recoverAgentStreamGoroutine(agentID, "revocation-watcher", cancel)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if g.deps.ShouldTerminateForRevocation(ctx, agentID, presentedSerial) {
					cancel()
					return
				}
			}
		}
	}()
}
