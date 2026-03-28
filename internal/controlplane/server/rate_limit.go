package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const staleRateLimitWindowMultiplier = 2

type fixedWindowRateLimiter struct {
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

func newFixedWindowRateLimiter(limit int, window time.Duration) *fixedWindowRateLimiter {
	if limit <= 0 {
		return nil
	}
	if window <= 0 {
		window = time.Minute
	}

	return &fixedWindowRateLimiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]rateLimitBucket),
	}
}

func (l *fixedWindowRateLimiter) Allow(key string, now time.Time) bool {
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

func (l *fixedWindowRateLimiter) cleanupStaleBucketsLocked(nowNanos int64) {
	if len(l.buckets) < 1024 {
		return
	}

	maxAge := int64(staleRateLimitWindowMultiplier) * l.window.Nanoseconds()
	cutoff := nowNanos - maxAge
	for key, bucket := range l.buckets {
		if bucket.lastSeen < cutoff {
			delete(l.buckets, key)
		}
	}
}

func (s *Server) withRateLimit(limiter *fixedWindowRateLimiter, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}
			key := ""
			if keyFn != nil {
				key = keyFn(r)
			}
			if !limiter.Allow(key, s.now()) {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestClientRateLimitKey(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		forwardedFor := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0])
		if forwardedFor != "" && remoteAddrTrustsForwardedFor(host) {
			return forwardedFor
		}
		if strings.TrimSpace(host) != "" {
			return host
		}
	}

	return strings.TrimSpace(r.RemoteAddr)
}

func remoteAddrTrustsForwardedFor(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}

	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}
