package server

import (
	"sync"
	"time"
)

const (
	accountLockoutMaxAttempts = 5
	accountLockoutDuration   = 15 * time.Minute
)

// accountLockoutTracker tracks consecutive failed login attempts per username
// and temporarily locks accounts after too many failures.
type accountLockoutTracker struct {
	mu       sync.Mutex
	accounts map[string]lockoutEntry
}

type lockoutEntry struct {
	failures int
	lockedAt time.Time
}

func newAccountLockoutTracker() *accountLockoutTracker {
	return &accountLockoutTracker{
		accounts: make(map[string]lockoutEntry),
	}
}

// IsLocked returns true if the account is currently locked out.
func (t *accountLockoutTracker) IsLocked(username string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.accounts[username]
	if !ok {
		return false
	}
	if entry.failures < accountLockoutMaxAttempts {
		return false
	}
	if now.Sub(entry.lockedAt) >= accountLockoutDuration {
		delete(t.accounts, username)
		return false
	}
	return true
}

// RecordFailure increments the failure counter for a username.
func (t *accountLockoutTracker) RecordFailure(username string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.accounts[username]
	entry.failures++
	if entry.failures >= accountLockoutMaxAttempts {
		entry.lockedAt = now
	}
	t.accounts[username] = entry

	t.cleanupLocked(now)
}

// CheckAndRecordFailure atomically checks lockout and records a failure.
// Returns true if the account is locked (failure is NOT recorded when locked).
func (t *accountLockoutTracker) CheckAndRecordFailure(username string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.accounts[username]
	if ok && entry.failures >= accountLockoutMaxAttempts {
		if now.Sub(entry.lockedAt) < accountLockoutDuration {
			return true
		}
		entry = lockoutEntry{}
	}

	entry.failures++
	if entry.failures >= accountLockoutMaxAttempts {
		entry.lockedAt = now
	}
	t.accounts[username] = entry
	t.cleanupLocked(now)
	return false
}

// ActiveCount returns the number of usernames whose account is currently
// locked out. Used by the metrics subsystem to expose panvex_lockout_active.
func (t *accountLockoutTracker) ActiveCount(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	count := 0
	for _, entry := range t.accounts {
		if entry.failures < accountLockoutMaxAttempts {
			continue
		}
		if now.Sub(entry.lockedAt) >= accountLockoutDuration {
			continue
		}
		count++
	}
	return count
}

// RecordSuccess clears the failure counter after a successful login.
func (t *accountLockoutTracker) RecordSuccess(username string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.accounts, username)
}

func (t *accountLockoutTracker) cleanupLocked(now time.Time) {
	if len(t.accounts) < 64 {
		return
	}
	for username, entry := range t.accounts {
		if entry.failures >= accountLockoutMaxAttempts && now.Sub(entry.lockedAt) >= accountLockoutDuration {
			delete(t.accounts, username)
		}
	}
}
