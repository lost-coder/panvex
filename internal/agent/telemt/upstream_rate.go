package telemt

import (
	"sync"
	"time"
)

// upstreamRateSample is one moment-in-time observation of cumulative counters.
type upstreamRateSample struct {
	ts       time.Time
	counters UpstreamCounters
}

// UpstreamRateTracker computes the failure rate over a rolling 5-minute window
// from cumulative counters. It owns a fixed-capacity ring buffer; Push is O(1)
// and Rate scans newest-to-oldest until it finds the youngest sample at least
// windowMin older than the latest. Counter resets (delta < 0) clear the ring.
type UpstreamRateTracker struct {
	mu        sync.Mutex
	samples   []upstreamRateSample
	head      int
	count     int
	cap       int
	windowMin time.Duration
	windowMax time.Duration
}

// NewUpstreamRateTracker returns a tracker with capacity for `cap` samples and
// a target rate window bounded by [windowMin, windowMax]. Recommended config
// for default 15s heartbeats: cap=32, windowMin=30s, windowMax=6m.
func NewUpstreamRateTracker(cap int, windowMin, windowMax time.Duration) *UpstreamRateTracker {
	return &UpstreamRateTracker{
		samples:   make([]upstreamRateSample, cap),
		cap:       cap,
		windowMin: windowMin,
		windowMax: windowMax,
	}
}

// Push adds a sample. If the new counters are smaller than the previous tail
// (telemt restart), the ring is cleared first so post-reset accumulation
// restarts cleanly.
func (t *UpstreamRateTracker) Push(now time.Time, c UpstreamCounters) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.count > 0 {
		prev := t.tailLocked()
		if isCounterReset(prev.counters, c) {
			t.clearLocked()
		}
	}

	t.samples[t.head] = upstreamRateSample{ts: now, counters: c}
	t.head = (t.head + 1) % t.cap
	if t.count < t.cap {
		t.count++
	}
}

// Rate returns the fail-rate percentage and whether the value is meaningful.
// Returns (0, false) when:
//   - the ring has fewer than 2 samples,
//   - the youngest valid base sample is younger than windowMin,
//   - the oldest sample is older than windowMax (ring is stale),
//   - counters indicate a reset that has not yet been overwritten.
func (t *UpstreamRateTracker) Rate() (pct float64, known bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.count < 2 {
		return 0, false
	}

	latest := t.tailLocked()
	base, ok := t.findBaseLocked(latest.ts)
	if !ok {
		return 0, false
	}

	deltaAttempt := int64(latest.counters.Attempt) - int64(base.counters.Attempt)
	deltaFail := int64(latest.counters.Fail+latest.counters.Failfast) -
		int64(base.counters.Fail+base.counters.Failfast)

	if deltaAttempt < 0 || deltaFail < 0 {
		return 0, false
	}
	if deltaAttempt == 0 {
		return 0, true
	}
	return float64(deltaFail) / float64(deltaAttempt) * 100, true
}

// findBaseLocked returns the oldest sample whose age (relative to latestTs)
// is at least windowMin and at most windowMax. Walks oldest→newest, skipping
// samples older than windowMax and bailing once we cross under windowMin.
func (t *UpstreamRateTracker) findBaseLocked(latestTs time.Time) (upstreamRateSample, bool) {
	for i := t.count - 1; i >= 0; i-- {
		idx := (t.head - 1 - i + t.cap) % t.cap
		s := t.samples[idx]
		age := latestTs.Sub(s.ts)
		if age > t.windowMax {
			continue
		}
		if age < t.windowMin {
			return upstreamRateSample{}, false
		}
		return s, true
	}
	return upstreamRateSample{}, false
}

func (t *UpstreamRateTracker) tailLocked() upstreamRateSample {
	idx := (t.head - 1 + t.cap) % t.cap
	return t.samples[idx]
}

func (t *UpstreamRateTracker) clearLocked() {
	t.head = 0
	t.count = 0
}

func isCounterReset(prev, curr UpstreamCounters) bool {
	return curr.Attempt < prev.Attempt ||
		curr.Success < prev.Success ||
		curr.Fail < prev.Fail ||
		curr.Failfast < prev.Failfast
}
