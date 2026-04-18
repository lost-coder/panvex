package sessions

import (
	"sync"
	"testing"
	"time"
)

func TestRateLimiterZeroLimitReturnsNil(t *testing.T) {
	if got := NewRateLimiter(0, time.Minute); got != nil {
		t.Fatalf("NewRateLimiter(0, ...) = %v, want nil", got)
	}
	if got := NewRateLimiter(-1, time.Minute); got != nil {
		t.Fatalf("NewRateLimiter(-1, ...) = %v, want nil", got)
	}
}

func TestRateLimiterNilAlwaysAllows(t *testing.T) {
	var l *RateLimiter // disabled limiter sentinel
	now := time.Unix(0, 0).UTC()
	for i := 0; i < 100; i++ {
		if !l.Allow("x", now) {
			t.Fatal("nil limiter denied a request, want always-allow")
		}
	}
}

func TestRateLimiterAllowsUpToLimit(t *testing.T) {
	l := NewRateLimiter(3, time.Minute)
	now := time.Unix(0, 0).UTC()
	for i := 0; i < 3; i++ {
		if !l.Allow("alice", now) {
			t.Fatalf("Allow call %d = false, want true", i+1)
		}
	}
	if l.Allow("alice", now) {
		t.Fatal("Allow call 4 = true, want false (over limit)")
	}
}

func TestRateLimiterResetsAcrossWindows(t *testing.T) {
	l := NewRateLimiter(2, time.Minute)
	now := time.Unix(0, 0).UTC()
	_ = l.Allow("alice", now)
	_ = l.Allow("alice", now)
	if l.Allow("alice", now) {
		t.Fatal("expected denial in same window")
	}
	next := now.Add(time.Minute)
	if !l.Allow("alice", next) {
		t.Fatal("expected allow in new window")
	}
}

func TestRateLimiterKeysAreIsolated(t *testing.T) {
	l := NewRateLimiter(1, time.Minute)
	now := time.Unix(0, 0).UTC()
	if !l.Allow("alice", now) {
		t.Fatal("alice first call denied")
	}
	if !l.Allow("bob", now) {
		t.Fatal("bob first call denied (should be independent bucket)")
	}
	if l.Allow("alice", now) {
		t.Fatal("alice second call allowed, want denied")
	}
}

func TestRateLimiterEmptyKeyCollapsesToSharedBucket(t *testing.T) {
	l := NewRateLimiter(1, time.Minute)
	now := time.Unix(0, 0).UTC()
	if !l.Allow("", now) {
		t.Fatal("first empty-key call denied")
	}
	if l.Allow("   ", now) {
		t.Fatal("second empty-key call allowed; expected to share bucket with first")
	}
}

func TestRateLimiterConcurrentAllowIsRaceFree(t *testing.T) {
	l := NewRateLimiter(1000, time.Minute)
	now := time.Unix(0, 0).UTC()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				l.Allow("alice", now)
				_ = id
			}
		}(i)
	}
	wg.Wait()
}
