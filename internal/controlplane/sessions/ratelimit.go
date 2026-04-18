package sessions

import (
	"strings"
	"sync"
	"time"
)

// StaleRateLimitWindowMultiplier controls how many window-widths a bucket
// is retained before the cleanup sweep discards it.
const StaleRateLimitWindowMultiplier = 2

// RateLimiter is a fixed-window rate limiter keyed by an arbitrary
// string (typically a client IP or "user:<id>" key).
//
// Concurrency: all methods are safe for use by multiple goroutines.
// A nil *RateLimiter treats every call as allowed, which lets callers
// wire an "off" limiter by passing nil instead of branching at every
// call site.
type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]rateLimitBucket
}

type rateLimitBucket struct {
	windowStart int64
	count       int
	lastSeen    int64
}

// NewRateLimiter constructs a fixed-window RateLimiter that allows at
// most `limit` requests per `window`. A non-positive limit returns nil
// (an always-allowed sentinel); a non-positive window falls back to
// one minute.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		return nil
	}
	if window <= 0 {
		window = time.Minute
	}

	return &RateLimiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]rateLimitBucket),
	}
}

// Allow reports whether the caller keyed by `key` may proceed at time
// `now`. A nil receiver always returns true, preserving the "disabled
// limiter" calling convention.
func (l *RateLimiter) Allow(key string, now time.Time) bool {
	if l == nil {
		return true
	}

	windowSize := l.window.Nanoseconds()
	if windowSize <= 0 {
		windowSize = time.Minute.Nanoseconds()
	}

	nowNanos := now.UTC().UnixNano()
	windowStart := (nowNanos / windowSize) * windowSize
	if strings.TrimSpace(key) == "" {
		// Fall back to a shared bucket for empty keys so they cannot
		// bypass the limiter by omitting the key.
		key = "unknown"
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, ok := l.buckets[key]
	if !ok || bucket.windowStart != windowStart {
		l.buckets[key] = rateLimitBucket{
			windowStart: windowStart,
			count:       1,
			lastSeen:    nowNanos,
		}
		l.cleanupStaleBucketsLocked(nowNanos)
		return true
	}
	if bucket.count >= l.limit {
		bucket.lastSeen = nowNanos
		l.buckets[key] = bucket
		l.cleanupStaleBucketsLocked(nowNanos)
		return false
	}

	bucket.count++
	bucket.lastSeen = nowNanos
	l.buckets[key] = bucket
	l.cleanupStaleBucketsLocked(nowNanos)
	return true
}

func (l *RateLimiter) cleanupStaleBucketsLocked(nowNanos int64) {
	if len(l.buckets) < 128 {
		return
	}

	maxAge := int64(StaleRateLimitWindowMultiplier) * l.window.Nanoseconds()
	cutoff := nowNanos - maxAge
	for key, bucket := range l.buckets {
		if bucket.lastSeen < cutoff {
			delete(l.buckets, key)
		}
	}
}
