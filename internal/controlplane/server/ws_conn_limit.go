package server

import (
	"sync"
	"sync/atomic"
)

// Per-key WebSocket connection caps for /events. Goroutine exhaustion
// is the main concern: every accepted connection holds a reader goroutine,
// a writer goroutine, and an event-bus subscription. An attacker with
// stolen session credentials (or even a misbehaving SPA) could otherwise
// open thousands of /events sockets and starve the server.
//
// Caps:
//   - maxWSConnsPerUser: per authenticated user-id. The dashboard normally
//     opens 1-2 sockets; 8 leaves headroom for multi-tab usage and a
//     stale-but-not-yet-cleaned-up reconnect.
//   - maxWSConnsPerIP: only used for unauthenticated callers (which today
//     means /events handler rejects them at requireSession; included for
//     defence-in-depth if the auth check is ever moved). Higher because a
//     single NAT can legitimately host many users.
const (
	maxWSConnsPerUser = 8
	maxWSConnsPerIP   = 32
)

// wsConnLimiter tracks live /events connections per key. The map stores
// *int32 counters mutated with sync/atomic so the inc/dec on a hot
// reconnect loop does not serialise on a single mutex.
//
// The map itself is guarded by a sync.Mutex — rare write events (insert
// new key, drop a counter that hit zero) only. The hot path is a read +
// atomic.AddInt32 on the existing pointer.
type wsConnLimiter struct {
	mu      sync.Mutex
	counts  map[string]*int32
}

func newWSConnLimiter() *wsConnLimiter {
	return &wsConnLimiter{counts: make(map[string]*int32)}
}

// acquire bumps the counter for key. If the post-increment value exceeds
// limit, acquire decrements back and returns false. The caller must call
// release(key) iff acquire returned true.
func (l *wsConnLimiter) acquire(key string, limit int32) bool {
	if l == nil || key == "" || limit <= 0 {
		return true
	}
	l.mu.Lock()
	counter, ok := l.counts[key]
	if !ok {
		var c int32
		counter = &c
		l.counts[key] = counter
	}
	l.mu.Unlock()

	if atomic.AddInt32(counter, 1) > limit {
		atomic.AddInt32(counter, -1)
		return false
	}
	return true
}

// release decrements the counter for key. Drops the map entry if the
// counter hits zero so the map does not grow unbounded for transient
// keys (e.g. one-off IPs).
func (l *wsConnLimiter) release(key string) {
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	counter, ok := l.counts[key]
	if !ok {
		return
	}
	if atomic.AddInt32(counter, -1) <= 0 {
		delete(l.counts, key)
	}
}

// snapshot returns the current count for key. Test-only helper — never
// called from production code.
func (l *wsConnLimiter) snapshot(key string) int32 {
	if l == nil || key == "" {
		return 0
	}
	l.mu.Lock()
	counter, ok := l.counts[key]
	l.mu.Unlock()
	if !ok {
		return 0
	}
	return atomic.LoadInt32(counter)
}
