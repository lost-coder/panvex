package sessions

import (
	"context"
	"sync"
	"time"
)

// TOTPLockoutMaxAttempts is the consecutive-failure threshold at which a
// TOTP-enabled account is temporarily locked out for the second factor
// (S-6). Stricter than LockoutMaxAttempts so an attacker who already
// guessed the password cannot indefinitely brute the 6-digit code at
// LockoutMaxAttempts/LockoutDuration (~480 codes/day) — see audit S-6.
const TOTPLockoutMaxAttempts = 3

// TOTPLockoutDuration is how long a TOTP-failed account stays locked
// after reaching TOTPLockoutMaxAttempts consecutive failures. Shorter
// than LockoutDuration because the legitimate user typically just needs
// to wait for the next 30 s code window after fat-fingering it.
const TOTPLockoutDuration = 5 * time.Minute

// TOTPLockoutTracker tracks consecutive failed TOTP-code attempts per
// username and locks the second factor independently of the password
// lockout (S-6). The two counters never share state — a wrong password
// does NOT bump the TOTP counter and vice versa — so an attacker who
// holds a valid password gets at most TOTPLockoutMaxAttempts code
// guesses per TOTPLockoutDuration window, not the LockoutMaxAttempts
// budget the password tracker grants.
//
// State is in-memory only by design: a 6-digit TOTP code has a 30-90 s
// validity window and the user must produce a fresh code on retry, so
// preserving the failure counter across a control-plane restart adds
// no security value. Restart resets the counter; the cache is rebuilt
// from live attempts.
//
// All methods are safe for concurrent use.
type TOTPLockoutTracker struct {
	mu       sync.Mutex
	accounts map[string]totpLockoutEntry
	max      int
	window   time.Duration
	// windowFn, when non-nil, overrides window on each evaluation so that
	// operator changes to auth.totp_lockout_duration take effect without a
	// panel restart. Set via SetThresholds.
	windowFn func() time.Duration

	// shards mirrors LockoutTracker.shards: 16 per-username serialisation
	// mutexes used by AttemptLock to close the IsLocked → verify →
	// RecordFailure race on a single username.
	shards [lockoutShardCount]sync.Mutex
}

type totpLockoutEntry struct {
	failures int
	lockedAt time.Time
}

// NewTOTPLockoutTracker constructs a fresh, empty TOTP failure tracker
// with the production thresholds (3 attempts / 5 min).
func NewTOTPLockoutTracker() *TOTPLockoutTracker {
	return &TOTPLockoutTracker{
		accounts: make(map[string]totpLockoutEntry),
		max:      TOTPLockoutMaxAttempts,
		window:   TOTPLockoutDuration,
	}
}

// SetThresholds wires a live getter for the lockout window and fixes the
// max-attempts threshold. windowFn is called on every lockout evaluation
// so that operator changes to auth.totp_lockout_duration take effect
// without restarting the control-plane. maxAttempts is fixed at construction
// time (TOTPLockoutMaxAttempts is not an audited tunable). Safe to call
// from any goroutine.
func (t *TOTPLockoutTracker) SetThresholds(maxAttempts int, windowFn func() time.Duration) {
	t.mu.Lock()
	t.max = maxAttempts
	t.windowFn = windowFn
	t.mu.Unlock()
}

// effectiveWindowLocked returns the current lockout window.
// Caller must hold t.mu.
func (t *TOTPLockoutTracker) effectiveWindowLocked() time.Duration {
	if t.windowFn != nil {
		return t.windowFn()
	}
	return t.window
}

// AttemptLock acquires a per-username serialisation lock that closes
// the IsLocked → verify → RecordFailure race on the second-factor path.
// Mirrors LockoutTracker.AttemptLock so callers can use the two
// trackers symmetrically.
func (t *TOTPLockoutTracker) AttemptLock(username string) func() {
	shard := lockoutShardFor(username)
	t.shards[shard].Lock()
	return t.shards[shard].Unlock
}

// IsLocked reports whether the TOTP second factor is currently locked
// for the given username. Use IsLockedWithContext from request handlers.
func (t *TOTPLockoutTracker) IsLocked(username string, now time.Time) bool {
	return t.IsLockedWithContext(context.Background(), username, now)
}

// IsLockedWithContext is the ctx-aware variant of IsLocked. Context is
// accepted for API symmetry with LockoutTracker; the in-memory tracker
// performs no I/O.
func (t *TOTPLockoutTracker) IsLockedWithContext(_ context.Context, username string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.accounts[username]
	if !ok {
		return false
	}
	if entry.failures < t.max {
		return false
	}
	if now.Sub(entry.lockedAt) >= t.effectiveWindowLocked() {
		delete(t.accounts, username)
		return false
	}
	return true
}

// RecordFailure increments the failure counter for a username on a
// wrong TOTP code. Use RecordFailureWithContext from request handlers.
func (t *TOTPLockoutTracker) RecordFailure(username string, now time.Time) {
	t.RecordFailureWithContext(context.Background(), username, now)
}

// RecordFailureWithContext is the ctx-aware variant of RecordFailure.
func (t *TOTPLockoutTracker) RecordFailureWithContext(_ context.Context, username string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.accounts[username]
	entry.failures++
	if entry.failures >= t.max {
		entry.lockedAt = now
	}
	t.accounts[username] = entry
	t.cleanupLocked(now)
}


// CheckAndRecordFailure atomically checks lockout and records a failure.
// Returns true if the account is locked (failure is NOT recorded when
// already locked). Use CheckAndRecordFailureWithContext from request
// handlers.
func (t *TOTPLockoutTracker) CheckAndRecordFailure(username string, now time.Time) bool {
	return t.CheckAndRecordFailureWithContext(context.Background(), username, now)
}

// CheckAndRecordFailureWithContext is the ctx-aware variant of
// CheckAndRecordFailure.
func (t *TOTPLockoutTracker) CheckAndRecordFailureWithContext(_ context.Context, username string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	window := t.effectiveWindowLocked()
	entry, ok := t.accounts[username]
	if ok && entry.failures >= t.max {
		if now.Sub(entry.lockedAt) < window {
			return true
		}
		entry = totpLockoutEntry{}
	}

	entry.failures++
	if entry.failures >= t.max {
		entry.lockedAt = now
	}
	t.accounts[username] = entry
	t.cleanupLocked(now)
	return false
}

// RecordSuccess clears the failure counter after a successful TOTP
// verification. Use RecordSuccessWithContext from request handlers.
func (t *TOTPLockoutTracker) RecordSuccess(username string) {
	t.RecordSuccessWithContext(context.Background(), username)
}

// RecordSuccessWithContext is the ctx-aware variant of RecordSuccess.
func (t *TOTPLockoutTracker) RecordSuccessWithContext(_ context.Context, username string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.accounts, username)
}

// ActiveCount returns the number of usernames currently TOTP-locked.
// Used by the metrics subsystem.
func (t *TOTPLockoutTracker) ActiveCount(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	window := t.effectiveWindowLocked()
	count := 0
	for _, entry := range t.accounts {
		if entry.failures < t.max {
			continue
		}
		if now.Sub(entry.lockedAt) >= window {
			continue
		}
		count++
	}
	return count
}

func (t *TOTPLockoutTracker) cleanupLocked(now time.Time) {
	if len(t.accounts) < 64 {
		return
	}
	window := t.effectiveWindowLocked()
	for username, entry := range t.accounts {
		if entry.failures >= t.max && now.Sub(entry.lockedAt) >= window {
			delete(t.accounts, username)
		}
	}
}
