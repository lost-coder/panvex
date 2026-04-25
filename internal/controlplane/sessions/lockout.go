package sessions

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// LockoutStore persists the lockout state so the failure counter
// survives control-plane restart and fail-over (S7). Kept as a local
// interface so the sessions package does not import the storage
// package directly — the server wires it in via SetStore.
type LockoutStore interface {
	UpsertLoginLockout(ctx context.Context, record LockoutRecord) error
	GetLoginLockout(ctx context.Context, username string) (LockoutRecord, error)
	DeleteLoginLockout(ctx context.Context, username string) error
	ListLoginLockouts(ctx context.Context) ([]LockoutRecord, error)
	DeleteExpiredLoginLockouts(ctx context.Context, before time.Time) (int64, error)
}

// LockoutRecord is the wire shape used between the tracker and any
// attached LockoutStore. Mirrors storage.LoginLockoutRecord 1:1 so
// adapters at the wiring seam are trivial field-copies.
type LockoutRecord struct {
	Username  string
	Failures  int
	LockedAt  *time.Time
	UpdatedAt time.Time
}

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
//
// Persistence (S7): when a LockoutStore is attached via SetStore, every
// mutation (failure record, release after window, success reset) is
// mirrored to the store synchronously. The in-memory map stays the hot
// path for reads; the store is the source of truth across restarts.
// Store errors are logged but never mask the in-memory result — the
// security property "locked in memory" is preserved even if the DB is
// briefly unavailable, we just lose durability for that window.
type LockoutTracker struct {
	mu       sync.Mutex
	accounts map[string]lockoutEntry
	store    LockoutStore
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

// SetStore attaches a persistent backend. Safe to call once at startup
// before any login traffic; subsequent calls replace the backend.
func (t *LockoutTracker) SetStore(store LockoutStore) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.store = store
}

// Restore loads the persisted lockout state into memory (S7). Should
// be called after SetStore during server bootstrap. Records older than
// LockoutDuration for a locked account are skipped so an expired
// lockout does not silently resurrect on restart.
func (t *LockoutTracker) Restore(ctx context.Context, now time.Time) error {
	t.mu.Lock()
	store := t.store
	t.mu.Unlock()
	if store == nil {
		return nil
	}
	records, err := store.ListLoginLockouts(ctx)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	for _, record := range records {
		entry := lockoutEntry{failures: record.Failures}
		if record.LockedAt != nil {
			if now.Sub(*record.LockedAt) >= LockoutDuration {
				continue
			}
			entry.lockedAt = *record.LockedAt
		}
		t.accounts[record.Username] = entry
	}
	return nil
}

// persistLocked writes the current state for username to the attached
// store (if any). Caller must hold t.mu. Errors are logged but not
// returned; callers already hold the lock and any failure is an
// availability issue, not a correctness issue for the local process.
func (t *LockoutTracker) persistLocked(ctx context.Context, username string, entry lockoutEntry) {
	if t.store == nil {
		return
	}
	record := LockoutRecord{
		Username:  username,
		Failures:  entry.failures,
		UpdatedAt: time.Now().UTC(),
	}
	if !entry.lockedAt.IsZero() {
		lockedAt := entry.lockedAt.UTC()
		record.LockedAt = &lockedAt
	}
	if err := t.store.UpsertLoginLockout(ctx, record); err != nil {
		slog.Warn("sessions: failed to persist login lockout", "username", username, "error", err)
	}
}

func (t *LockoutTracker) deletePersistedLocked(ctx context.Context, username string) {
	if t.store == nil {
		return
	}
	if err := t.store.DeleteLoginLockout(ctx, username); err != nil {
		slog.Warn("sessions: failed to delete login lockout", "username", username, "error", err)
	}
}

// IsLocked returns true if the account is currently locked out.
//
// Deprecated: prefer IsLockedWithContext from request handlers so the
// underlying store delete (when an expired lockout is reaped) inherits
// request cancellation.
func (t *LockoutTracker) IsLocked(username string, now time.Time) bool {
	return t.IsLockedWithContext(context.Background(), username, now)
}

// IsLockedWithContext is the ctx-aware variant of IsLocked.
func (t *LockoutTracker) IsLockedWithContext(ctx context.Context, username string, now time.Time) bool {
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
		t.deletePersistedLocked(ctx, username)
		return false
	}
	return true
}

// RecordFailure increments the failure counter for a username.
//
// Deprecated: prefer RecordFailureWithContext from request handlers.
func (t *LockoutTracker) RecordFailure(username string, now time.Time) {
	t.RecordFailureWithContext(context.Background(), username, now)
}

// RecordFailureWithContext is the ctx-aware variant of RecordFailure.
func (t *LockoutTracker) RecordFailureWithContext(ctx context.Context, username string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.accounts[username]
	entry.failures++
	if entry.failures >= LockoutMaxAttempts {
		entry.lockedAt = now
	}
	t.accounts[username] = entry
	t.persistLocked(ctx, username, entry)

	t.cleanupLocked(ctx, now)
}

// CheckAndRecordFailure atomically checks lockout and records a failure.
// Returns true if the account is locked (failure is NOT recorded when locked).
//
// Deprecated: prefer CheckAndRecordFailureWithContext from request handlers.
func (t *LockoutTracker) CheckAndRecordFailure(username string, now time.Time) bool {
	return t.CheckAndRecordFailureWithContext(context.Background(), username, now)
}

// CheckAndRecordFailureWithContext is the ctx-aware variant of
// CheckAndRecordFailure.
func (t *LockoutTracker) CheckAndRecordFailureWithContext(ctx context.Context, username string, now time.Time) bool {
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
	t.persistLocked(ctx, username, entry)
	t.cleanupLocked(ctx, now)
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
//
// Deprecated: prefer RecordSuccessWithContext from request handlers.
func (t *LockoutTracker) RecordSuccess(username string) {
	t.RecordSuccessWithContext(context.Background(), username)
}

// RecordSuccessWithContext is the ctx-aware variant of RecordSuccess.
func (t *LockoutTracker) RecordSuccessWithContext(ctx context.Context, username string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.accounts, username)
	t.deletePersistedLocked(ctx, username)
}

func (t *LockoutTracker) cleanupLocked(ctx context.Context, now time.Time) {
	if len(t.accounts) < 64 {
		return
	}
	for username, entry := range t.accounts {
		if entry.failures >= LockoutMaxAttempts && now.Sub(entry.lockedAt) >= LockoutDuration {
			delete(t.accounts, username)
			t.deletePersistedLocked(ctx, username)
		}
	}
}
