package server

import (
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// TestAgentSelfUpdateJobInputHasTTL guards A3: a self-update job without a
// TTL never expires (jobs.Service treats TTL<=0 as never-expiring), so a
// wedged update is re-dispatched every retry window forever.
func TestAgentSelfUpdateJobInputHasTTL(t *testing.T) {
	in := agentSelfUpdateJobInput("agent-1", `{"version":"1.2.3"}`, "user-1")
	if in.TTL <= 0 {
		t.Fatalf("self-update job must carry a positive TTL, got %v", in.TTL)
	}
	if in.TTL != 10*time.Minute {
		t.Fatalf("TTL = %v, want %v", in.TTL, 10*time.Minute)
	}
	if in.Action != jobs.ActionAgentSelfUpdate {
		t.Fatalf("Action = %q, want %q", in.Action, jobs.ActionAgentSelfUpdate)
	}
	if len(in.TargetAgentIDs) != 1 || in.TargetAgentIDs[0] != "agent-1" {
		t.Fatalf("TargetAgentIDs = %v, want [agent-1]", in.TargetAgentIDs)
	}
	if in.PayloadJSON != `{"version":"1.2.3"}` {
		t.Fatalf("PayloadJSON = %q", in.PayloadJSON)
	}
	if in.ActorID != "user-1" {
		t.Fatalf("ActorID = %q, want user-1", in.ActorID)
	}
}
