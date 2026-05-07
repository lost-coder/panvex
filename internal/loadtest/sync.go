// Package loadtest hosts the realistic concurrency scenarios that
// exercise the control-plane subsystems under representative agent-fleet
// load. The scenarios live in *_test.go files; this file holds shared
// helpers reused across them.
//
// The harness deliberately stays at the service / storage layer (matching
// the existing load_bench_test.go) instead of booting the full HTTP+gRPC
// stack: those layers require internal test fixtures (CSRF secrets,
// fleet-group seeding, mTLS authority bootstrap) that live behind unexported
// helpers in package server. Exercising the publicly-exported services
// (auth, jobs) plus the SQLite-backed Store is the hot path every HTTP
// route ultimately drives.
package loadtest

import (
	"sort"
	"sync"
	"testing"
	"time"
)

// eventually polls cond every 5ms until it returns true or the deadline
// elapses. Returns true on success, false on timeout. tb is *testing.T or
// *testing.B; both implement testing.TB. Modelled after assertEventually
// patterns used elsewhere in the codebase but kept tiny here so the
// loadtest package retains its zero-extra-dependency property.
func eventually(tb testing.TB, timeout time.Duration, cond func() bool) bool {
	tb.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// latencySamples is a tiny lock-protected buffer of observed latencies.
// Sized for the scenarios in this package (low thousands of samples max);
// not intended as a general-purpose histogram. Use Record from any
// goroutine and Percentile from a single observer once the load phase has
// quiesced.
type latencySamples struct {
	mu      sync.Mutex
	samples []time.Duration
}

// Record stores one observed latency. Safe for concurrent use.
func (s *latencySamples) Record(d time.Duration) {
	s.mu.Lock()
	s.samples = append(s.samples, d)
	s.mu.Unlock()
}

// Snapshot returns a sorted copy of the recorded samples. Caller owns the
// slice. Returns an empty slice when nothing has been recorded.
func (s *latencySamples) Snapshot() []time.Duration {
	s.mu.Lock()
	out := make([]time.Duration, len(s.samples))
	copy(out, s.samples)
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Percentile returns the requested percentile (0..1) latency from the
// recorded samples. Returns 0 when no samples have been recorded.
// Implementation uses nearest-rank — exact within ±1 sample, sufficient
// for regression detection at this scale.
func (s *latencySamples) Percentile(p float64) time.Duration {
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	sorted := s.Snapshot()
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

// Len reports how many samples have been recorded.
func (s *latencySamples) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.samples)
}
