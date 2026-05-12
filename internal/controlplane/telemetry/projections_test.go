package telemetry

import (
	"strings"
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
		TelemtUnreachable:       false,
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
		UseMiddleProxy:          true,
		MERuntimeReady:          true,
		TelemtUnreachable:       false,
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
		{"coverage_0_not_reported", 0, false, "ok", ""},
		{"coverage_50_reported", 50, true, "warn", "DC coverage is degraded"},
		{"coverage_50_not_reported", 50, false, "warn", "DC coverage is degraded"},
		{"coverage_100_reported", 100, true, "ok", ""},
		{"coverage_100_not_reported", 100, false, "ok", ""},
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

func TestSeverityAndReasonDirectMatrix(t *testing.T) {
	fresh := Freshness{State: "fresh"}
	base := SeverityInput{
		PresenceState:           presence.StateOnline,
		AgentReported:           true,
		AcceptingNewConnections: true,
		UseMiddleProxy:          false,
		UptimeSeconds:           120, // past 60s grace
		TelemtUnreachable:       false,
	}

	cases := []struct {
		name            string
		healthy         int
		total           int
		rate            float64
		rateKnown       bool
		wantSeverity    string
		wantReasonExact string
	}{
		{"all_healthy_no_rate", 3, 3, 0, false, "ok", ""},
		{"some_unhealthy", 2, 3, 0, false, "warn", "some upstreams unhealthy"},
		{"all_down", 0, 3, 0, false, "critical", "all upstreams down"},
		{"none_configured", 0, 0, 0, false, "warn", "no upstreams configured"},
		{"rate_below_warn", 3, 3, 5, true, "ok", ""},
		{"rate_warn_band", 3, 3, 25, true, "warn", "degraded DC connectivity"},
		{"rate_critical_band", 3, 3, 60, true, "critical", "upstream DC connect failing"},
		{"rate_unknown_falls_to_health", 0, 3, 0, false, "critical", "all upstreams down"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			in.HealthyUpstreams = tc.healthy
			in.TotalUpstreams = tc.total
			in.UpstreamFailRatePct5m = tc.rate
			in.UpstreamFailRateKnown = tc.rateKnown

			sev, reason := SeverityAndReason(in, fresh)
			if sev != tc.wantSeverity {
				t.Fatalf("severity = %q, want %q", sev, tc.wantSeverity)
			}
			if reason != tc.wantReasonExact {
				t.Fatalf("reason = %q, want %q", reason, tc.wantReasonExact)
			}
		})
	}
}

func TestSeverityAndReasonDirectGracePeriod(t *testing.T) {
	fresh := Freshness{State: "fresh"}
	in := SeverityInput{
		PresenceState:           presence.StateOnline,
		AgentReported:           true,
		AcceptingNewConnections: true,
		UseMiddleProxy:          false,
		UptimeSeconds:           30, // before 60s grace
		HealthyUpstreams:        0,
		TotalUpstreams:          3,
		TelemtUnreachable:       false,
	}
	sev, _ := SeverityAndReason(in, fresh)
	if sev != "ok" {
		t.Fatalf("severity = %q during 60s grace, want ok", sev)
	}
}

func TestSeverityAndReasonMeDown(t *testing.T) {
	fresh := Freshness{State: "fresh"}
	in := SeverityInput{
		PresenceState:           presence.StateOnline,
		AgentReported:           true,
		AcceptingNewConnections: true,
		UseMiddleProxy:          true,
		MERuntimeReady:          false,
		ME2DCFallbackEnabled:    false,
		UptimeSeconds:           120,
		TelemtUnreachable:       false,
	}
	sev, reason := SeverityAndReason(in, fresh)
	if sev != "critical" {
		t.Fatalf("severity = %q, want critical", sev)
	}
	if reason != "ME pool unavailable, traffic stopped" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestClassifyMode(t *testing.T) {
	cases := []struct {
		name       string
		useME      bool
		meReady    bool
		fallbackOn bool
		want       ModeKind
	}{
		{"direct_by_config", false, false, false, ModeDirect},
		{"direct_by_config_meflag_ignored_when_disabled", false, true, true, ModeDirect},
		{"me_normal", true, true, false, ModeME},
		{"me_normal_with_fallback_flag", true, true, true, ModeME},
		{"fallback_active", true, false, true, ModeFallback},
		{"me_down_no_fallback", true, false, false, ModeMeDown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := SeverityInput{
				UseMiddleProxy:       tc.useME,
				MERuntimeReady:       tc.meReady,
				ME2DCFallbackEnabled: tc.fallbackOn,
				TelemtUnreachable:    false,
			}
			got := ClassifyMode(in)
			if got != tc.want {
				t.Fatalf("ClassifyMode = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSeverityAndReasonFallbackMatrix(t *testing.T) {
	fresh := Freshness{State: "fresh"}
	base := SeverityInput{
		PresenceState:           presence.StateOnline,
		AgentReported:           true,
		AcceptingNewConnections: true,
		UseMiddleProxy:          true,
		MERuntimeReady:          false,
		ME2DCFallbackEnabled:    true,
		UptimeSeconds:           120,
		HealthyUpstreams:        3,
		TotalUpstreams:          3,
		TelemtUnreachable:       false,
	}

	t.Run("baseline_warn", func(t *testing.T) {
		in := base
		in.FallbackActiveDuration = 5 * time.Minute
		sev, reason := SeverityAndReason(in, fresh)
		if sev != "warn" {
			t.Fatalf("severity = %q, want warn", sev)
		}
		if reason != "running on ME→Direct fallback" {
			t.Fatalf("reason = %q", reason)
		}
	})

	t.Run("escalates_after_30min", func(t *testing.T) {
		in := base
		in.FallbackActiveDuration = 31 * time.Minute
		sev, reason := SeverityAndReason(in, fresh)
		if sev != "critical" {
			t.Fatalf("severity = %q, want critical", sev)
		}
		if !strings.Contains(reason, "ME pool down, fallback active") {
			t.Fatalf("reason = %q, want prefix 'ME pool down, fallback active'", reason)
		}
	})

	t.Run("direct_critical_keeps_fallback_suffix", func(t *testing.T) {
		in := base
		in.FallbackActiveDuration = 5 * time.Minute
		in.HealthyUpstreams = 0 // direct rule says critical
		sev, reason := SeverityAndReason(in, fresh)
		if sev != "critical" {
			t.Fatalf("severity = %q, want critical", sev)
		}
		if !strings.HasPrefix(reason, "all upstreams down") {
			t.Fatalf("reason = %q, want prefix 'all upstreams down'", reason)
		}
		if !strings.Contains(reason, "(on ME→Direct fallback)") {
			t.Fatalf("reason = %q, want suffix '(on ME→Direct fallback)'", reason)
		}
	})

	t.Run("escalation_combines_with_baseline_reason", func(t *testing.T) {
		in := base
		in.FallbackActiveDuration = 31 * time.Minute
		in.HealthyUpstreams = 2 // direct rule says warn "some upstreams unhealthy"
		sev, reason := SeverityAndReason(in, fresh)
		if sev != "critical" {
			t.Fatalf("severity = %q, want critical", sev)
		}
		if !strings.Contains(reason, "some upstreams unhealthy") {
			t.Fatalf("reason = %q, want to contain baseline reason", reason)
		}
	})
}

func TestSeverityAndReason_TelemtUnreachable_Critical(t *testing.T) {
	in := SeverityInput{
		PresenceState:              presence.StateOnline,
		AcceptingNewConnections:    true,
		TelemtUnreachable:          true,
		TelemtUnreachableSinceUnix: 1700000000,
	}
	fresh := Freshness{State: "fresh", ObservedAtUnix: 1700000050}
	sev, reason := SeverityAndReason(in, fresh)
	if sev != "critical" {
		t.Fatalf("severity = %q, want critical", sev)
	}
	if !strings.Contains(reason, "Telemt API unreachable since") {
		t.Fatalf("reason = %q, want substring \"Telemt API unreachable since\"", reason)
	}
	// 1700000000 in UTC → "2023-11-14T22:13:20Z"
	if !strings.Contains(reason, "2023-11-14T22:13:20Z") {
		t.Fatalf("reason = %q missing formatted timestamp", reason)
	}
}

func TestSeverityAndReason_OfflinePresenceWinsOverTelemtUnreachable(t *testing.T) {
	in := SeverityInput{
		PresenceState:     presence.StateOffline,
		TelemtUnreachable: true,
	}
	fresh := Freshness{State: "fresh"}
	sev, reason := SeverityAndReason(in, fresh)
	if sev != "bad" {
		t.Fatalf("severity = %q, want bad (offline takes precedence)", sev)
	}
	if reason != "Agent heartbeat is offline" {
		t.Fatalf("reason = %q, want \"Agent heartbeat is offline\"", reason)
	}
}

func TestSeverityAndReason_TelemtUnreachable_BeatsStaleFreshness(t *testing.T) {
	in := SeverityInput{
		PresenceState:              presence.StateOnline,
		TelemtUnreachable:          true,
		TelemtUnreachableSinceUnix: 1700000000,
	}
	fresh := Freshness{State: "stale", ObservedAtUnix: 1699000000}
	sev, _ := SeverityAndReason(in, fresh)
	if sev != "critical" {
		t.Fatalf("severity = %q, want critical (telemt unreachable beats stale)", sev)
	}
}
