package sessions

import (
	"context"
	"sync"
	"time"
)

// IP-keyed lockout thresholds. The username-keyed LockoutTracker can be
// weaponised for targeted DoS: an attacker enumerates usernames and
// triggers 5 failures against each to lock every account in turn.
// Adding an IP-keyed counter blunts that attack — a single source IP
// gets a wider but still finite budget across all usernames before the
// IP itself is rate-limited (S-medium).
//
// Numbers tuned so legitimate fat-fingering at a small office NAT (10
// users x 5 fails before username-lockout = 50) sits at the edge of the
// threshold and a real attacker hits it well within the window.
const (
	// IPLockoutMaxFailures is the rolling-window failure budget for one IP.
	IPLockoutMaxFailures = 50
	// IPLockoutWindow is the sliding window over which failures
	// accumulate towards IPLockoutMaxFailures.
	IPLockoutWindow = 15 * time.Minute
	// IPLockoutDuration is how long an IP stays locked after exceeding
	// the failure budget. Longer than IPLockoutWindow on purpose: once
	// an IP shows credential-stuffing behaviour the appropriate response
	// is a meaningful timeout, not a quick reset.
	IPLockoutDuration = 30 * time.Minute
)

// IPLockoutTracker counts failed login attempts per source IP over a
// rolling window and locks the IP for a fixed duration when the budget
// is exceeded. It runs PARALLEL to LockoutTracker (username-keyed) and
// TOTPLockoutTracker (second-factor) — the three counters never share
// state.
//
// State is in-memory only. An IP-lockout that survives a control-plane
// restart adds little value: a determined attacker rotates IPs anyway,
// and persistence here would require a new storage table for what is
// fundamentally a transient anti-abuse signal. Restart resets the
// counters.
//
// All methods are safe for concurrent use.
type IPLockoutTracker struct {
	mu         sync.Mutex
	entries    map[string]*ipLockoutEntry
	maxFails   int
	window     time.Duration
	lockoutDur time.Duration
}

// ipLockoutEntry holds the per-IP state. failures is a slice of
// timestamps, kept sorted by insertion order (which is monotonic
// because clocks only ever move forward in our handlers — Server.now()
// is a single source). lockedUntil is non-zero while the IP is in the
// post-budget timeout.
type ipLockoutEntry struct {
	failures    []time.Time
	lockedUntil time.Time
}

// NewIPLockoutTracker constructs an empty IP failure tracker with the
// production thresholds (50 failures / 15 min window / 30 min lockout).
func NewIPLockoutTracker() *IPLockoutTracker {
	return &IPLockoutTracker{
		entries:    make(map[string]*ipLockoutEntry),
		maxFails:   IPLockoutMaxFailures,
		window:     IPLockoutWindow,
		lockoutDur: IPLockoutDuration,
	}
}

// IsLocked reports whether the source IP is currently in the post-budget
// timeout. Use IsLockedWithContext from request handlers.
func (t *IPLockoutTracker) IsLocked(ip string, now time.Time) bool {
	return t.IsLockedWithContext(context.Background(), ip, now)
}

// IsLockedWithContext is the ctx-aware variant. ctx is accepted for API
// symmetry with LockoutTracker; the in-memory tracker performs no I/O.
func (t *IPLockoutTracker) IsLockedWithContext(_ context.Context, ip string, now time.Time) bool {
	if ip == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.entries[ip]
	if !ok {
		return false
	}
	if entry.lockedUntil.IsZero() {
		return false
	}
	if !now.Before(entry.lockedUntil) {
		// Lockout window expired — drop the entry entirely so the
		// caller starts with a fresh budget.
		delete(t.entries, ip)
		return false
	}
	return true
}

// RecordFailure logs a failure for the source IP at "now". If the
// rolling-window count reaches the budget, the IP transitions to the
// locked state for IPLockoutDuration. Use RecordFailureWithContext from
// request handlers.
func (t *IPLockoutTracker) RecordFailure(ip string, now time.Time) {
	t.RecordFailureWithContext(context.Background(), ip, now)
}

// RecordFailureWithContext is the ctx-aware variant.
func (t *IPLockoutTracker) RecordFailureWithContext(_ context.Context, ip string, now time.Time) {
	if ip == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.entries[ip]
	if !ok {
		entry = &ipLockoutEntry{}
		t.entries[ip] = entry
	}

	// If the IP is still in lockdown, do not extend nor reset — the
	// timeout is already running. Counting further failures here would
	// either bump the counter past the budget (no-op) or, worse, allow
	// the attacker to push the lockout end-time forward.
	if !entry.lockedUntil.IsZero() && now.Before(entry.lockedUntil) {
		t.cleanupLocked(now)
		return
	}
	// Lockout expired but entry was retained — reset before counting.
	if !entry.lockedUntil.IsZero() && !now.Before(entry.lockedUntil) {
		entry.lockedUntil = time.Time{}
		entry.failures = entry.failures[:0]
	}

	entry.failures = pruneOlderThan(entry.failures, now.Add(-t.window))
	entry.failures = append(entry.failures, now)
	if len(entry.failures) >= t.maxFails {
		entry.lockedUntil = now.Add(t.lockoutDur)
		// Drop the timestamp slice once locked — we only need the
		// lockedUntil deadline from here on, and the slice would
		// otherwise grow unbounded for a chatty attacker.
		entry.failures = nil
	}
	t.cleanupLocked(now)
}

// CheckAndRecordFailure atomically checks and records. Returns true if
// the IP is already locked (failure is NOT recorded in that case). Use
// CheckAndRecordFailureWithContext from request handlers.
func (t *IPLockoutTracker) CheckAndRecordFailure(ip string, now time.Time) bool {
	return t.CheckAndRecordFailureWithContext(context.Background(), ip, now)
}

// CheckAndRecordFailureWithContext is the ctx-aware variant.
func (t *IPLockoutTracker) CheckAndRecordFailureWithContext(_ context.Context, ip string, now time.Time) bool {
	if ip == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.entries[ip]
	if ok && !entry.lockedUntil.IsZero() {
		if now.Before(entry.lockedUntil) {
			return true
		}
		// Expired lockout — reset and fall through to record this
		// failure as the first one of a new window.
		entry.lockedUntil = time.Time{}
		entry.failures = entry.failures[:0]
	}
	if !ok {
		entry = &ipLockoutEntry{}
		t.entries[ip] = entry
	}

	entry.failures = pruneOlderThan(entry.failures, now.Add(-t.window))
	entry.failures = append(entry.failures, now)
	if len(entry.failures) >= t.maxFails {
		entry.lockedUntil = now.Add(t.lockoutDur)
		entry.failures = nil
	}
	t.cleanupLocked(now)
	return false
}

// ActiveCount returns the number of IPs currently in the locked state.
// Used by the metrics subsystem.
func (t *IPLockoutTracker) ActiveCount(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	count := 0
	for _, entry := range t.entries {
		if entry.lockedUntil.IsZero() {
			continue
		}
		if now.Before(entry.lockedUntil) {
			count++
		}
	}
	return count
}

// pruneOlderThan returns the input slice trimmed to entries whose
// timestamp is at or after cutoff. Mutates the underlying array.
func pruneOlderThan(stamps []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(stamps) && stamps[first].Before(cutoff) {
		first++
	}
	if first == 0 {
		return stamps
	}
	// Shift surviving entries to the head so the array can be reused.
	n := copy(stamps, stamps[first:])
	return stamps[:n]
}

// cleanupLocked drops entries that are neither in-window nor in lockout.
// Caller must hold t.mu. Cheap (one map iteration) but only run past a
// soft size threshold so steady-state traffic does not pay the cost.
func (t *IPLockoutTracker) cleanupLocked(now time.Time) {
	if len(t.entries) < 64 {
		return
	}
	cutoff := now.Add(-t.window)
	for ip, entry := range t.entries {
		if !entry.lockedUntil.IsZero() && now.Before(entry.lockedUntil) {
			continue
		}
		entry.failures = pruneOlderThan(entry.failures, cutoff)
		if len(entry.failures) == 0 && entry.lockedUntil.IsZero() {
			delete(t.entries, ip)
		}
	}
}
