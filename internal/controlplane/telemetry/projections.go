package telemetry

import (
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/presence"
)

// ModeKind classifies the operating mode of a Telemt node from runtime flags.
type ModeKind int

const (
	ModeME ModeKind = iota
	ModeDirect
	ModeFallback
	ModeMeDown
)

func (m ModeKind) String() string {
	switch m {
	case ModeME:
		return "me"
	case ModeDirect:
		return "direct"
	case ModeFallback:
		return "fallback"
	case ModeMeDown:
		return "me_down"
	}
	return "unknown"
}

// SeverityInput describes the operator-facing runtime state used for severity decisions.
type SeverityInput struct {
	PresenceState           presence.State
	ReadOnly                bool
	AcceptingNewConnections bool
	Degraded                bool
	StartupStatus           string
	DCCoveragePct           float64
	HealthyUpstreams        int
	TotalUpstreams          int
	// AgentReported is true when the agent has delivered at least one runtime
	// snapshot. Distinguishes "zero coverage because all DCs are dead" (critical)
	// from "zero coverage because we have no data yet" (neutral default).
	AgentReported bool

	UseMiddleProxy       bool
	MERuntimeReady       bool
	ME2DCFallbackEnabled bool
	UptimeSeconds        float64

	UpstreamFailRatePct5m float64
	UpstreamFailRateKnown bool

	FallbackActiveDuration time.Duration
}

// ClassifyMode derives the operating mode from runtime flags. Used by the
// severity projector and by the dashboard to pick the right detail layout.
func ClassifyMode(in SeverityInput) ModeKind {
	if !in.UseMiddleProxy {
		return ModeDirect
	}
	if in.MERuntimeReady {
		return ModeME
	}
	if in.ME2DCFallbackEnabled {
		return ModeFallback
	}
	return ModeMeDown
}

// FreshnessForObservedAt normalizes runtime freshness from an observed timestamp.
func FreshnessForObservedAt(observedAt time.Time, now time.Time, staleAfter time.Duration) Freshness {
	if observedAt.IsZero() {
		return Freshness{State: "never_collected", ObservedAtUnix: 0}
	}
	if now.UTC().Sub(observedAt.UTC()) > staleAfter {
		return Freshness{State: "stale", ObservedAtUnix: observedAt.UTC().Unix()}
	}
	return Freshness{State: "fresh", ObservedAtUnix: observedAt.UTC().Unix()}
}

// DetailBoostState normalizes one detail boost window.
func DetailBoostState(expiresAt, now time.Time) DetailBoost {
	if expiresAt.IsZero() || !expiresAt.After(now) {
		return DetailBoost{}
	}
	return DetailBoost{
		Active:           true,
		ExpiresAtUnix:    expiresAt.UTC().Unix(),
		RemainingSeconds: int64(expiresAt.UTC().Sub(now.UTC()).Seconds()),
	}
}

// SeverityAndReason derives one operator-facing severity and primary reason.
func SeverityAndReason(input SeverityInput, freshness Freshness) (string, string) {
	switch {
	case input.PresenceState == presence.StateOffline:
		return "bad", "Agent heartbeat is offline"
	case freshness.State == "stale":
		return "warn", "Telemetry is stale"
	case input.ReadOnly:
		return "warn", "Telemt API is read-only"
	case !input.AcceptingNewConnections:
		return "warn", "Admission is closed"
	case input.Degraded:
		return "warn", "Runtime is degraded"
	case input.StartupStatus != "" && input.StartupStatus != "ready":
		return "warn", "Startup is still in progress"
	}

	switch ClassifyMode(input) {
	case ModeME:
		return severityME(input)
	case ModeDirect:
		return severityDirect(input)
	case ModeFallback:
		return severityFallback(input)
	case ModeMeDown:
		return "critical", "ME pool unavailable, traffic stopped"
	}
	return "ok", ""
}

// severityME applies ME-mode severity rules. Caller has already excluded
// offline / not-accepting / read-only / startup branches.
func severityME(in SeverityInput) (severity, reason string) {
	switch {
	case in.AgentReported && in.DCCoveragePct == 0:
		return "critical", "no reachable DCs"
	case in.DCCoveragePct > 0 && in.DCCoveragePct < 100:
		return "warn", "DC coverage is degraded"
	}
	return "ok", ""
}

// severityDirect implements the rules for nodes running with use_middle_proxy=false.
// Caller has already excluded the offline / not-accepting cases.
func severityDirect(in SeverityInput) (severity, reason string) {
	if in.UpstreamFailRateKnown {
		switch {
		case in.UpstreamFailRatePct5m >= 50:
			return "critical", "upstream DC connect failing"
		case in.UpstreamFailRatePct5m >= 10:
			return "warn", "degraded DC connectivity"
		}
	}

	if in.UptimeSeconds < 60 {
		return "ok", ""
	}

	switch {
	case in.TotalUpstreams == 0:
		return "warn", "no upstreams configured"
	case in.HealthyUpstreams == 0:
		return "critical", "all upstreams down"
	case in.HealthyUpstreams < in.TotalUpstreams:
		return "warn", "some upstreams unhealthy"
	}
	return "ok", ""
}

// severityFallback is a temporary placeholder; Task 3.5 implements the real rules.
func severityFallback(in SeverityInput) (severity, reason string) {
	return "warn", "running on ME→Direct fallback"
}

// SeverityRank orders server summaries by severity.
func SeverityRank(value string) int {
	switch value {
	case "critical":
		return 4
	case "bad":
		return 3
	case "warn":
		return 2
	default:
		return 1
	}
}
