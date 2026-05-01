package telemt

import (
	"testing"
	"time"
)

func TestUpstreamRateUnknownBeforeFirstSample(t *testing.T) {
	r := NewUpstreamRateTracker(32, 30*time.Second, 6*time.Minute)
	pct, known := r.Rate()
	if known {
		t.Fatalf("Rate() known = true before any Push, want false")
	}
	if pct != 0 {
		t.Fatalf("Rate() pct = %v, want 0", pct)
	}
}

func TestUpstreamRateUnknownWhenWindowTooSmall(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewUpstreamRateTracker(32, 30*time.Second, 6*time.Minute)
	r.Push(base, UpstreamCounters{Attempt: 100, Fail: 5})
	r.Push(base.Add(10*time.Second), UpstreamCounters{Attempt: 110, Fail: 6})
	_, known := r.Rate()
	if known {
		t.Fatalf("Rate() known = true with 10s window (<min 30s), want false")
	}
}

func TestUpstreamRateComputesAcrossFiveMinuteWindow(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewUpstreamRateTracker(32, 30*time.Second, 6*time.Minute)
	// 21 samples over 5 minutes (15-second cadence).
	for i := 0; i <= 20; i++ {
		r.Push(
			base.Add(time.Duration(i)*15*time.Second),
			UpstreamCounters{
				Attempt:  uint64(100 * (i + 1)),
				Fail:     uint64(10 * (i + 1)),
				Failfast: 0,
			},
		)
	}
	pct, known := r.Rate()
	if !known {
		t.Fatalf("Rate() known = false, want true after 5 minutes of samples")
	}
	// delta_attempt = 100*21 - 100 = 2000; delta_fail = 10*21 - 10 = 200
	// → 200/2000 = 10%
	if pct < 9.5 || pct > 10.5 {
		t.Fatalf("Rate() pct = %v, want ~10", pct)
	}
}

func TestUpstreamRateInvalidatesOnCounterReset(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewUpstreamRateTracker(32, 30*time.Second, 6*time.Minute)
	r.Push(base, UpstreamCounters{Attempt: 1000, Fail: 100})
	r.Push(base.Add(time.Minute), UpstreamCounters{Attempt: 50, Fail: 5}) // reset
	_, known := r.Rate()
	if known {
		t.Fatalf("Rate() known = true after counter reset, want false")
	}
	// After reset, the ring should restart from the new sample.
	r.Push(base.Add(2*time.Minute), UpstreamCounters{Attempt: 100, Fail: 10})
	r.Push(base.Add(3*time.Minute), UpstreamCounters{Attempt: 200, Fail: 25})
	pct, known := r.Rate()
	if !known {
		t.Fatalf("Rate() known = false after re-accumulation, want true")
	}
	// delta_attempt = 200-50 = 150; delta_fail = 25-5 = 20
	// → 20/150 ≈ 13.33%
	if pct < 13 || pct > 14 {
		t.Fatalf("Rate() pct = %v, want ~13.33", pct)
	}
}

func TestUpstreamRateUnknownWhenAllSamplesTooStale(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := NewUpstreamRateTracker(8, 30*time.Second, 2*time.Minute) // tight max for the test
	r.Push(base, UpstreamCounters{Attempt: 100, Fail: 5})
	// Big jump — only one sample, and it's older than windowMax.
	r.Push(base.Add(10*time.Minute), UpstreamCounters{Attempt: 200, Fail: 10})
	_, known := r.Rate()
	if known {
		t.Fatalf("Rate() known = true when only stale samples remain, want false")
	}
}
