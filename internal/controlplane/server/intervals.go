package server

import "time"

// Q5.U-Q-04: typed config for the intervals + caps the audit flagged
// as scattered magic constants. They land in one file so an operator
// who needs to tune a fleet has a single grep target instead of
// hunting through server.go / timeseries_rollup.go / batch_writer.go.
//
// The values still match the legacy constants — this commit centralises
// them, it does not retune them. A follow-up PR can expose any of
// these via PanelSettings if the audit's "configurable for different
// loads" goal needs to be operator-toggleable.

// Intervals bundles the worker / poller cadences that are inherent to
// the control-plane lifecycle (not tied to a single feature).
type Intervals struct {
	// JobsKeyEviction sets how often the jobs service scans for
	// terminal-state idempotency keys to evict.
	JobsKeyEviction time.Duration
	// JobsKeyEvictionTTL is the age at which a terminal-state key is
	// evicted.
	JobsKeyEvictionTTL time.Duration
	// JobsAckExpiry sets how often the jobs service scans for
	// acknowledged-but-result-less targets to expire.
	JobsAckExpiry time.Duration
	// Rollup is how often the timeseries rollup worker fires.
	Rollup time.Duration
	// CertExpiryWatch is the certificate-expiry sweep cadence.
	CertExpiryWatch time.Duration
	// PoolStats is the database pool-stats sample period.
	PoolStats time.Duration
}

// DefaultIntervals returns the values matching the legacy constants
// (jobsKeyEvictionInterval, jobsAckExpiryInterval, rollupInterval, …).
// Tests that need a fast clock can construct an Intervals literal
// directly.
func DefaultIntervals() Intervals {
	return Intervals{
		JobsKeyEviction:    time.Hour,
		JobsKeyEvictionTTL: 24 * time.Hour,
		JobsAckExpiry:      time.Minute,
		Rollup:             5 * time.Minute,
		CertExpiryWatch:    time.Hour,
		PoolStats:          15 * time.Second,
	}
}
