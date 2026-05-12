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

	// TelemtUnreachable is the agent's last reported unreachability of its
	// local Telemt API. Default false on the wire (proto3 bool default) =
	// healthy/reachable — explicit true (with the timestamp below) is the
	// panel's signal to surface a critical "Telemt API unreachable" reason
	// instead of mis-classifying the node as Direct.
	TelemtUnreachable          bool
	TelemtUnreachableSinceUnix int64
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
	case input.TelemtUnreachable:
		return "critical", "Telemt API unreachable since " + formatTelemtSince(input.TelemtUnreachableSinceUnix)
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

// severityFallback applies fallback-mode severity rules. The baseline is the
// max(direct severity, "warn"); the reason carries the underlying direct
// failure with a fallback suffix so the attention-list keeps fallback
// context visible. ≥30 min duration escalates baseline to critical with a
// two-part reason.
//
// Rank-collapse note: the 30-min boundary primarily upgrades a "warn"
// baseline (e.g. ok/warn direct) to "critical". When directSev is already
// "critical" (e.g. all upstreams down) the rank stays "critical" on both
// sides of the boundary — only the reason changes from
// "<directReason> (on ME→Direct fallback)" to
// "ME pool down, fallback active — <directReason> (on ME→Direct fallback)".
// This is intentional: a critical-direct condition is already at max
// severity; the boundary just adds the ME-pool context to the reason.
func severityFallback(in SeverityInput) (severity, reason string) {
	directSev, directReason := severityDirect(in)
	baselineSev := maxSeverity(directSev, "warn")
	var baselineReason string
	switch directSev {
	case "ok":
		baselineReason = "running on ME→Direct fallback"
	case "warn":
		baselineReason = directReason + " (on ME→Direct fallback)"
	default: // critical or other
		baselineReason = directReason + " (on ME→Direct fallback)"
	}

	if in.FallbackActiveDuration >= 30*time.Minute {
		return "critical", "ME pool down, fallback active — " + baselineReason
	}
	return baselineSev, baselineReason
}

func maxSeverity(a, b string) string {
	rank := func(s string) int {
		switch s {
		case "ok":
			return 0
		case "warn":
			return 1
		case "critical":
			return 2
		case "bad":
			return 3
		}
		return 0
	}
	if rank(a) >= rank(b) {
		return a
	}
	return b
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

// formatTelemtSince renders the unreachable-since timestamp as RFC3339 UTC.
// Returns "unknown time" when the unix value is zero — defensive; in
// practice the agent always sets it when reachable=false.
func formatTelemtSince(unix int64) string {
	if unix <= 0 {
		return "unknown time"
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}
