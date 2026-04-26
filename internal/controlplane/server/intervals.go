package server

import "time"

// Intervals bundles the worker / poller cadences that are inherent to
// the control-plane lifecycle (not tied to a single feature).
//
// Q5.U-Q-04 introduced this struct; R-Q-04 wires it through Options
// and Server so operators (and tests) can override individual cadences
// without rebuilding the binary or threading new constants through
// every callsite. The defaults match the legacy package-level values.
type Intervals struct {
	// JobsKeyEviction is how often the jobs service scans for
	// terminal-state idempotency keys to evict.
	JobsKeyEviction time.Duration
	// JobsKeyEvictionTTL is the age at which a terminal-state key is
	// evicted.
	JobsKeyEvictionTTL time.Duration
	// JobsAckExpiry is how often the jobs service scans for
	// acknowledged-but-result-less targets.
	JobsAckExpiry time.Duration
	// JobsAckExpiryTTL is the threshold after which an acknowledged
	// target with no result is transitioned to expired. Must match
	// the agent-side idempotency cache.
	JobsAckExpiryTTL time.Duration
	// Rollup is how often the timeseries rollup worker fires.
	Rollup time.Duration
	// MetricsPoller is the cadence for sampling derived gauges
	// (agent connected count, event-hub subscribers, job queue depth,
	// lockout count, DB pool stats).
	MetricsPoller time.Duration
}

// DefaultIntervals returns the values matching the legacy package-level
// constants. Tests that need a fast clock can construct an Intervals
// literal directly and pass it through Options.Intervals.
func DefaultIntervals() Intervals {
	return Intervals{
		JobsKeyEviction:    time.Hour,
		JobsKeyEvictionTTL: 24 * time.Hour,
		JobsAckExpiry:      time.Hour,
		JobsAckExpiryTTL:   2 * time.Hour,
		Rollup:             5 * time.Minute,
		MetricsPoller:      5 * time.Second,
	}
}

// withDefaults fills in zero-valued fields from DefaultIntervals so
// callers can override only the cadences they care about.
func (i Intervals) withDefaults() Intervals {
	d := DefaultIntervals()
	if i.JobsKeyEviction == 0 {
		i.JobsKeyEviction = d.JobsKeyEviction
	}
	if i.JobsKeyEvictionTTL == 0 {
		i.JobsKeyEvictionTTL = d.JobsKeyEvictionTTL
	}
	if i.JobsAckExpiry == 0 {
		i.JobsAckExpiry = d.JobsAckExpiry
	}
	if i.JobsAckExpiryTTL == 0 {
		i.JobsAckExpiryTTL = d.JobsAckExpiryTTL
	}
	if i.Rollup == 0 {
		i.Rollup = d.Rollup
	}
	if i.MetricsPoller == 0 {
		i.MetricsPoller = d.MetricsPoller
	}
	return i
}
