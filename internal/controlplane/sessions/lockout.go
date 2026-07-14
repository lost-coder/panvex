package sessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
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
// and temporarily locks accounts after too many failures. The counting,
// threshold and cleanup mechanics live in counterLockout; this type adds
// what is specific to the password factor: durability and log redaction.
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
	counterLockout

	// cfgMu guards store + redactor. They are wired once at startup and read
	// from the persist hooks, which now run OUTSIDE the counter lock (R7), so
	// they need a guard of their own. It must never be held across a store
	// call — snapshot under it, then call.
	cfgMu sync.RWMutex
	store LockoutStore

	// redactor maps a raw username to the privacy-preserving identifier
	// used in slog warnings (R-S-09). Read/written under the core mutex so
	// SetRedactor is safe to call after construction; defaults to a
	// tracker-internal SHA-256 prefix so unwired tests never accidentally
	// leak raw usernames either.
	redactor func(string) string
}

// NewLockoutTracker constructs a fresh, empty LockoutTracker.
func NewLockoutTracker() *LockoutTracker {
	t := &LockoutTracker{
		counterLockout: newCounterLockout(LockoutMaxAttempts, LockoutDuration),
	}
	t.onPersist = t.persistEntry
	t.onDelete = t.deletePersisted
	return t
}

// SetThresholds wires live getter functions for the lockout thresholds.
// Each function is called on every lockout evaluation so operator changes
// to auth.password_lockout_max_attempts / auth.password_lockout_duration
// take effect without restarting the control-plane. Pass nil for either
// argument to keep the compiled-in constant as fallback.
func (t *LockoutTracker) SetThresholds(maxAttempts func() int, duration func() time.Duration) {
	t.mu.Lock()
	t.maxAttemptsFn = maxAttempts
	t.windowFn = duration
	t.mu.Unlock()
}

// SetStore attaches a persistent backend. Safe to call once at startup
// before any login traffic; subsequent calls replace the backend.
func (t *LockoutTracker) SetStore(store LockoutStore) {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	t.store = store
}

// SetRedactor installs the redaction function used for log fields that
// would otherwise carry raw usernames (R-S-09). Server wires this to
// its HMAC-prefix logUsername so production log aggregators see the
// same correlatable id used elsewhere.
func (t *LockoutTracker) SetRedactor(fn func(string) string) {
	t.cfgMu.Lock()
	defer t.cfgMu.Unlock()
	t.redactor = fn
}

// snapshotConfig returns the wired store and redactor. Callers use it to avoid
// holding cfgMu across the store round-trip.
func (t *LockoutTracker) snapshotConfig() (LockoutStore, func(string) string) {
	t.cfgMu.RLock()
	defer t.cfgMu.RUnlock()
	redactor := t.redactor
	if redactor == nil {
		redactor = defaultRedact
	}
	return t.store, redactor
}

func defaultRedact(username string) string {
	u := strings.TrimSpace(username)
	if u == "" {
		return "u-anon"
	}
	sum := sha256.Sum256([]byte(strings.ToLower(u)))
	return "u-" + hex.EncodeToString(sum[:6])
}

// Restore loads the persisted lockout state into memory (S7). Should
// be called after SetStore during server bootstrap. Records older than
// the lockout window are skipped so an expired lockout does not silently
// resurrect on restart.
func (t *LockoutTracker) Restore(ctx context.Context, now time.Time) error {
	store, _ := t.snapshotConfig()
	if store == nil {
		return nil
	}
	records, err := store.ListLoginLockouts(ctx)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	window := t.windowLocked()
	for _, record := range records {
		entry := lockoutEntry{failures: record.Failures}
		if record.LockedAt != nil {
			if now.Sub(*record.LockedAt) >= window {
				continue
			}
			entry.lockedAt = *record.LockedAt
		}
		t.entries[record.Username] = entry
	}
	return nil
}

// persistEntry is the counterLockout onPersist hook: it mirrors the current
// state for username to the attached store. Called WITHOUT the counter lock —
// this is a DB round-trip. Errors are logged, not returned: a store failure is
// an availability issue, not a correctness issue for the local process, which
// already holds the authoritative in-memory state.
func (t *LockoutTracker) persistEntry(ctx context.Context, username string, entry lockoutEntry) {
	store, redact := t.snapshotConfig()
	if store == nil {
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
	if err := store.UpsertLoginLockout(ctx, record); err != nil {
		slog.Warn("sessions: failed to persist login lockout", "username_hash", redact(username), "error", err)
	}
}

// deletePersisted is the counterLockout onDelete hook. Called WITHOUT the
// counter lock, like persistEntry.
func (t *LockoutTracker) deletePersisted(ctx context.Context, username string) {
	store, redact := t.snapshotConfig()
	if store == nil {
		return
	}
	if err := store.DeleteLoginLockout(ctx, username); err != nil {
		slog.Warn("sessions: failed to delete login lockout", "username_hash", redact(username), "error", err)
	}
}
