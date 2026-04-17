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

// TestSeverityAndReasonDCCoverageMatrix covers the DCCoveragePct x AgentReported
// combinations. Zero coverage from an agent that actually reported runtime is
// critical ("no reachable DCs"); zero coverage without any agent report is
// still the neutral default (P2-LOG-08 / M-C8).
func TestSeverityAndReasonDCCoverageMatrix(t *testing.T) {
	baseInput := SeverityInput{
		PresenceState:           presence.StateOnline,
		ReadOnly:                false,
		AcceptingNewConnections: true,
		Degraded:                false,
		StartupStatus:           "ready",
	}
	freshness := Freshness{State: "fresh"}

	cases := []struct {
		name          string
		coverage      float64
		agentReported bool
		wantSeverity  string
		wantReason    string
	}{
		{"coverage_0_reported", 0, true, "critical", "no reachable DCs"},
		{"coverage_0_not_reported", 0, false, "good", "Node is ready"},
		{"coverage_50_reported", 50, true, "warn", "DC coverage is degraded"},
		{"coverage_50_not_reported", 50, false, "warn", "DC coverage is degraded"},
		{"coverage_100_reported", 100, true, "good", "Node is ready"},
		{"coverage_100_not_reported", 100, false, "good", "Node is ready"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			input := baseInput
			input.DCCoveragePct = tc.coverage
			input.AgentReported = tc.agentReported
			severity, reason := SeverityAndReason(input, freshness)
			if severity != tc.wantSeverity {
				t.Fatalf("SeverityAndReason() severity = %q, want %q", severity, tc.wantSeverity)
			}
			if reason != tc.wantReason {
				t.Fatalf("SeverityAndReason() reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}
