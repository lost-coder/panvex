package sessions

import (
	"context"
	"sync"
	"time"
)

// lockoutShardCount is the number of per-key serialisation mutexes used by
// AttemptLock. Sharding keeps the lock cheap: different keys hash to
// different slots and never block each other except on collisions, which
// last a single auth attempt.
const lockoutShardCount = 16

// lockoutEntry is the per-key state of a counter lockout: how many
// consecutive failures we have seen and when the counter tripped.
// lockedAt is the zero value while failures < maxAttempts.
type lockoutEntry struct {
	failures int
	lockedAt time.Time
}

// counterLockout is the shared mechanics behind the two consecutive-failure
// trackers: LockoutTracker (password, persistent) and TOTPLockoutTracker
// (second factor, memory-only). A key trips after maxAttempts consecutive
// failures and stays locked for window from the LAST failure — recording a
// failure while already locked re-arms the window (pinned by
// TestLockoutRecordFailureExtendsDeadline).
//
// IPLockoutTracker deliberately does NOT build on this: it counts failures
// over a sliding window rather than consecutively, and a failure against an
// already-locked IP must not push the deadline out. Those are different
// mechanics, not a different configuration of these.
//
// All methods are safe for concurrent use. onPersist/onDelete are invoked
// with mu held and may be nil.
type counterLockout struct {
	mu      sync.Mutex
	entries map[string]lockoutEntry

	// maxAttempts / window are the compiled-in defaults. The *Fn variants,
	// when non-nil, override them on every evaluation so operator changes
	// take effect without a panel restart (see each tracker's SetThresholds).
	maxAttempts   int
	window        time.Duration
	maxAttemptsFn func() int
	windowFn      func() time.Duration

	// onPersist / onDelete mirror a mutation to durable storage. Nil for the
	// memory-only TOTP tracker. They are invoked WITHOUT mu held — a store
	// write is a DB round-trip, and running it inside the critical section
	// blocked every other login attempt for its duration (R7).
	//
	// Dropping the lock does not reorder writes for a given key: callers hold
	// AttemptLock (the per-key shard mutex) across the whole
	// IsLocked → verify → Record sequence, so two mutations of the same
	// account never overlap. Writes for DIFFERENT accounts may interleave,
	// which is exactly what we want — they are independent rows.
	onPersist func(ctx context.Context, key string, entry lockoutEntry)
	onDelete  func(ctx context.Context, key string)

	shards [lockoutShardCount]sync.Mutex
}

func newCounterLockout(maxAttempts int, window time.Duration) counterLockout {
	return counterLockout{
		entries:     make(map[string]lockoutEntry),
		maxAttempts: maxAttempts,
		window:      window,
	}
}

// maxAttemptsLocked returns the effective failure threshold. Caller holds mu.
func (t *counterLockout) maxAttemptsLocked() int {
	if t.maxAttemptsFn != nil {
		return t.maxAttemptsFn()
	}
	return t.maxAttempts
}

// windowLocked returns the effective lockout window. Caller holds mu.
func (t *counterLockout) windowLocked() time.Duration {
	if t.windowFn != nil {
		return t.windowFn()
	}
	return t.window
}

// AttemptLock acquires a per-key serialisation lock that closes the
// IsLocked → verify → RecordFailure race (Q2.U-S-15). Callers MUST invoke
// the returned release function once the verify+record sequence finishes.
func (t *counterLockout) AttemptLock(key string) func() {
	shard := lockoutShardFor(key)
	t.shards[shard].Lock()
	return t.shards[shard].Unlock
}

// lockoutShardFor returns the shard index for a key via FNV-1a 32-bit hash
// modulo the shard count. FNV is non-cryptographic but stable and
// zero-allocation, which fits a hot path.
func lockoutShardFor(key string) uint32 {
	var hash uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}
	return hash % lockoutShardCount
}

// storeOps is the durable work a mutation produced. It is collected under mu
// and executed after the lock is released — see onPersist/onDelete.
type storeOps struct {
	persistKey   string
	persistEntry lockoutEntry
	persist      bool
	deletes      []string
}

func (t *counterLockout) runOps(ctx context.Context, ops storeOps) {
	if ops.persist && t.onPersist != nil {
		t.onPersist(ctx, ops.persistKey, ops.persistEntry)
	}
	if t.onDelete == nil {
		return
	}
	for _, key := range ops.deletes {
		t.onDelete(ctx, key)
	}
}

// IsLockedWithContext reports whether key is currently locked out. An expired
// lockout is dropped (and unpersisted) on the way out so the caller starts
// with a fresh budget.
func (t *counterLockout) IsLockedWithContext(ctx context.Context, key string, now time.Time) bool {
	t.mu.Lock()
	entry, ok := t.entries[key]
	if !ok || entry.failures < t.maxAttemptsLocked() {
		t.mu.Unlock()
		return false
	}
	if now.Sub(entry.lockedAt) >= t.windowLocked() {
		delete(t.entries, key)
		t.mu.Unlock()
		t.runOps(ctx, storeOps{deletes: []string{key}})
		return false
	}
	t.mu.Unlock()
	return true
}

// RecordFailureWithContext increments the failure counter for key. Reaching
// the threshold (re-)arms the lockout window from now.
func (t *counterLockout) RecordFailureWithContext(ctx context.Context, key string, now time.Time) {
	t.mu.Lock()
	ops := t.bumpLocked(key, t.entries[key], now)
	ops.deletes = append(ops.deletes, t.cleanupLocked(now)...)
	t.mu.Unlock()

	t.runOps(ctx, ops)
}

// CheckAndRecordFailureWithContext reports whether key was ALREADY locked and,
// when it was not, records this failure. Returning true means the caller must
// reject the attempt without consuming budget; the single critical section
// closes the check → record race the two-call form leaves open.
func (t *counterLockout) CheckAndRecordFailureWithContext(ctx context.Context, key string, now time.Time) bool {
	t.mu.Lock()
	entry, ok := t.entries[key]
	if ok && entry.failures >= t.maxAttemptsLocked() {
		if now.Sub(entry.lockedAt) < t.windowLocked() {
			t.mu.Unlock()
			return true
		}
		// Expired lockout: start a fresh counter and fall through so this
		// failure is recorded as the first of the new window.
		entry = lockoutEntry{}
	}

	ops := t.bumpLocked(key, entry, now)
	ops.deletes = append(ops.deletes, t.cleanupLocked(now)...)
	t.mu.Unlock()

	t.runOps(ctx, ops)
	return false
}

// bumpLocked applies one failure to entry and returns the durable work it
// produced. Caller holds mu.
func (t *counterLockout) bumpLocked(key string, entry lockoutEntry, now time.Time) storeOps {
	entry.failures++
	if entry.failures >= t.maxAttemptsLocked() {
		entry.lockedAt = now
	}
	t.entries[key] = entry
	return storeOps{persistKey: key, persistEntry: entry, persist: true}
}

// RecordSuccessWithContext clears the failure counter after a successful
// verification.
func (t *counterLockout) RecordSuccessWithContext(ctx context.Context, key string) {
	t.mu.Lock()
	delete(t.entries, key)
	t.mu.Unlock()

	t.runOps(ctx, storeOps{deletes: []string{key}})
}

// ActiveCount returns the number of keys currently locked out. Used by the
// metrics subsystem.
func (t *counterLockout) ActiveCount(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	maxAttempts := t.maxAttemptsLocked()
	window := t.windowLocked()
	count := 0
	for _, entry := range t.entries {
		if entry.failures >= maxAttempts && now.Sub(entry.lockedAt) < window {
			count++
		}
	}
	return count
}

// cleanupLocked drops entries whose lockout has expired. Cheap (one map
// iteration) but only run past a soft size threshold so steady-state traffic
// does not pay the cost. Caller holds mu.
// cleanupLocked drops entries whose lockout has expired and returns their keys
// so the caller can unpersist them AFTER releasing the lock. Caller holds mu.
func (t *counterLockout) cleanupLocked(now time.Time) []string {
	if len(t.entries) < 64 {
		return nil
	}
	maxAttempts := t.maxAttemptsLocked()
	window := t.windowLocked()
	var evicted []string
	for key, entry := range t.entries {
		if entry.failures >= maxAttempts && now.Sub(entry.lockedAt) >= window {
			delete(t.entries, key)
			evicted = append(evicted, key)
		}
	}
	return evicted
}
