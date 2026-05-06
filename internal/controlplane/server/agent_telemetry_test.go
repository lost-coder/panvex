package server

import (
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func TestAgentRuntimeFromSnapshotPropagatesTelemtReachability(t *testing.T) {
	snap := &gatewayrpc.RuntimeSnapshot{
		TelemtReachable:            false,
		TelemtUnreachableSinceUnix: 1700000000,
	}
	out := agentRuntimeFromSnapshot(snap, time.Unix(1700000050, 0))
	if out.TelemtReachable {
		t.Fatal("TelemtReachable = true, want false")
	}
	if out.TelemtUnreachableSinceUnix != 1700000000 {
		t.Fatalf("TelemtUnreachableSinceUnix = %d, want 1700000000", out.TelemtUnreachableSinceUnix)
	}
}

func TestAgentRuntimeFromSnapshotPassesThroughReachableTrue(t *testing.T) {
	snap := &gatewayrpc.RuntimeSnapshot{
		UseMiddleProxy:             true,
		MeRuntimeReady:             true,
		TelemtReachable:            true,
		TelemtUnreachableSinceUnix: 0,
	}
	out := agentRuntimeFromSnapshot(snap, time.Unix(1700000050, 0))
	if !out.TelemtReachable {
		t.Fatal("TelemtReachable = false, want true (passthrough)")
	}
	if out.TelemtUnreachableSinceUnix != 0 {
		t.Fatalf("TelemtUnreachableSinceUnix = %d, want 0", out.TelemtUnreachableSinceUnix)
	}
}
