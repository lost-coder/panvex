package telemetry

import (
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/presence"
)

func TestFreshnessForObservedAtMarksStaleSnapshots(t *testing.T) {
	now := time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC)
	freshness := FreshnessForObservedAt(now.Add(-45*time.Second), now, 30*time.Second)

	if freshness.State != "stale" {
		t.Fatalf("FreshnessForObservedAt().State = %q, want %q", freshness.State, "stale")
	}
}

func TestDetailBoostStateMarksActiveWindow(t *testing.T) {
	now := time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC)
	boost := DetailBoostState(now.Add(10*time.Minute), now)

	if !boost.Active {
		t.Fatal("DetailBoostState().Active = false, want true")
	}
	if boost.RemainingSeconds <= 0 {
		t.Fatalf("DetailBoostState().RemainingSeconds = %d, want > 0", boost.RemainingSeconds)
	}
}

func TestSeverityAndReasonPrefersOfflineOverOtherSignals(t *testing.T) {
	freshness := Freshness{State: "fresh"}
	severity, reason := SeverityAndReason(SeverityInput{
		PresenceState:           presence.StateOffline,
		ReadOnly:                true,
		AcceptingNewConnections: false,
		Degraded:                true,
	}, freshness)

	if severity != "bad" {
		t.Fatalf("SeverityAndReason() severity = %q, want %q", severity, "bad")
	}
	if reason != "Agent heartbeat is offline" {
		t.Fatalf("SeverityAndReason() reason = %q, want %q", reason, "Agent heartbeat is offline")
	}
}
