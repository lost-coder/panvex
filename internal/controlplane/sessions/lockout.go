package sessions

import (
	"sync"
	"time"
)

// LockoutMaxAttempts is the consecutive-failure threshold at which an
// account becomes locked. Exported so tests and metrics callers can
// reason about the tripping point.
const LockoutMaxAttempts = 5

// LockoutDuration is how long an account stays locked after reaching
// LockoutMaxAttempts consecutive failures.
const LockoutDuration = 15 * time.Minute

// LockoutTracker tracks consecutive failed login attempts per username
// and temporarily locks accounts after too many failures.
//
// Concurrency: all methods are safe for use by multiple goroutines.
type LockoutTracker struct {
	mu       sync.Mutex
	accounts map[string]lockoutEntry
}

type lockoutEntry struct {
	failures int
	lockedAt time.Time
}

// NewLockoutTracker constructs a fresh, empty LockoutTracker.
func NewLockoutTracker() *LockoutTracker {
	return &LockoutTracker{
		accounts: make(map[string]lockoutEntry),
	}
}

// IsLocked returns true if the account is currently locked out.
func (t *LockoutTracker) IsLocked(username string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.accounts[username]
	if !ok {
		return false
	}
	if entry.failures < LockoutMaxAttempts {
		return false
	}
	if now.Sub(entry.lockedAt) >= LockoutDuration {
		delete(t.accounts, username)
		return false
	}
	return true
}

// RecordFailure increments the failure counter for a username.
func (t *LockoutTracker) RecordFailure(username string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.accounts[username]
	entry.failures++
	if entry.failures >= LockoutMaxAttempts {
		entry.lockedAt = now
	}
	t.accounts[username] = entry

	t.cleanupLocked(now)
}

// CheckAndRecordFailure atomically checks lockout and records a failure.
// Returns true if the account is locked (failure is NOT recorded when locked).
func (t *LockoutTracker) CheckAndRecordFailure(username string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.accounts[username]
	if ok && entry.failures >= LockoutMaxAttempts {
		if now.Sub(entry.lockedAt) < LockoutDuration {
			return true
		}
		entry = lockoutEntry{}
	}

	entry.failures++
	if entry.failures >= LockoutMaxAttempts {
		entry.lockedAt = now
	}
	t.accounts[username] = entry
	t.cleanupLocked(now)
	return false
}

// ActiveCount returns the number of usernames whose account is currently
// locked out. Used by the metrics subsystem to expose panvex_lockout_active.
func (t *LockoutTracker) ActiveCount(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	count := 0
	for _, entry := range t.accounts {
		if entry.failures < LockoutMaxAttempts {
			continue
		}
		if now.Sub(entry.lockedAt) >= LockoutDuration {
			continue
		}
		count++
	}
	return count
}

// RecordSuccess clears the failure counter after a successful login.
func (t *LockoutTracker) RecordSuccess(username string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.accounts, username)
}

func (t *LockoutTracker) cleanupLocked(now time.Time) {
	if len(t.accounts) < 64 {
		return
	}
	for username, entry := range t.accounts {
		if entry.failures >= LockoutMaxAttempts && now.Sub(entry.lockedAt) >= LockoutDuration {
			delete(t.accounts, username)
		}
	}
}
