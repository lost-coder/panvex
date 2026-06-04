package server

import (
	"context"
	"testing"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// A healthy→unreachable→healthy sequence must set the rediscovery flag on the
// agent's live session exactly on the recovery edge.
func TestApplyTelemtReachabilityTransitionRequestsRediscovery(t *testing.T) {
	s := mustNew(t, Options{LoginTimingFloor: -1})
	t.Cleanup(s.Close)

	const agentID = "agent-eu-1"
	sess, unregister := s.sessions.Register(agentID)
	t.Cleanup(unregister)

	apply := func(unreachable bool) {
		runtime := &gatewayrpc.RuntimeSnapshot{
			UseMiddleProxy:    true,
			MeRuntimeReady:    true,
			TelemtUnreachable: unreachable,
		}
		if err := s.applyAgentSnapshot(context.Background(), agentSnapshot{
			AgentID:    agentID,
			NodeName:   "node-eu-1",
			Version:    "1.0.0",
			Runtime:    runtime,
			HasRuntime: true,
			ObservedAt: s.now(),
		}); err != nil {
			t.Fatalf("applyAgentSnapshot() error = %v", err)
		}
	}

	apply(false) // first healthy — no edge
	if sess.TakeRediscovery() {
		t.Fatal("no rediscovery expected on first healthy snapshot")
	}
	apply(true) // outage — no edge
	if sess.TakeRediscovery() {
		t.Fatal("no rediscovery expected on outage")
	}
	apply(false) // recovery — edge!
	if !sess.TakeRediscovery() {
		t.Fatal("expected rediscovery to be requested on Telemt recovery")
	}
}
