package telemetry

import (
	"time"

	"github.com/panvex/panvex/internal/controlplane/presence"
)

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
func DetailBoostState(expiresAt time.Time, now time.Time) DetailBoost {
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
	case input.TotalUpstreams > 0 && input.HealthyUpstreams < input.TotalUpstreams:
		return "warn", "Some upstreams are unhealthy"
	case input.DCCoveragePct > 0 && input.DCCoveragePct < 100:
		return "warn", "DC coverage is degraded"
	default:
		return "good", "Node is ready"
	}
}

// SeverityRank orders server summaries by severity.
func SeverityRank(value string) int {
	switch value {
	case "bad":
		return 3
	case "warn":
		return 2
	default:
		return 1
	}
}
