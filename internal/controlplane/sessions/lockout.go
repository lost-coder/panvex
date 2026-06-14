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
// and temporarily locks accounts after too many failures.
//
// Concurrency: all methods are safe for use by multiple goroutines.
//
// The lockout thresholds default to LockoutMaxAttempts / LockoutDuration.
// Call SetThresholds to wire live getters from an OperationalStore so
// operator changes are picked up without restarting the control-plane.
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

	// redactor maps a raw username to the privacy-preserving identifier
	// used in slog warnings (R-S-09). Kept behind a mutex so SetRedactor
	// is safe to call after construction; defaults to a tracker-internal
	// SHA-256 prefix so unwired tests never accidentally leak raw
	// usernames either.
	redactor func(string) string

	// maxAttemptsFn / lockoutDurationFn, when non-nil, override the
	// LockoutMaxAttempts / LockoutDuration constants so that operator
	// changes via the OperationalStore are visible on the next auth
	// attempt without a panel restart. Set via SetThresholds.
	maxAttemptsFn     func() int
	lockoutDurationFn func() time.Duration

	// shards holds 16 attempt-mutexes used by AttemptLock to serialize
	// the read-verify-write sequence on a single username (Q2.U-S-15).
	// Sharding keeps the lock cheap — different users hash to different
	// slots and never block each other except on collisions, which are
	// short-lived (one auth attempt).
	shards [lockoutShardCount]sync.Mutex
}

// SetThresholds wires live getter functions for the lockout thresholds.
// Each function is called on every lockout evaluation so operator changes
// to auth.password_lockout_max_attempts / auth.password_lockout_duration
// take effect without restarting the control-plane. Pass nil for either
// argument to keep the compiled-in constant as fallback.
func (t *LockoutTracker) SetThresholds(maxAttempts func() int, duration func() time.Duration) {
	t.mu.Lock()
	t.maxAttemptsFn = maxAttempts
	t.lockoutDurationFn = duration
	t.mu.Unlock()
}

// maxAttemptsLocked returns the effective max-attempts threshold.
// Caller must hold t.mu.
func (t *LockoutTracker) maxAttemptsLocked() int {
	if t.maxAttemptsFn != nil {
		return t.maxAttemptsFn()
	}
	return LockoutMaxAttempts
}

// lockoutDurationLocked returns the effective lockout duration.
// Caller must hold t.mu.
func (t *LockoutTracker) lockoutDurationLocked() time.Duration {
	if t.lockoutDurationFn != nil {
		return t.lockoutDurationFn()
	}
	return LockoutDuration
}

const lockoutShardCount = 16

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

// AttemptLock acquires a per-username serialisation lock that closes
// the IsLocked → verify → RecordFailure race (Q2.U-S-15). Callers MUST
// invoke the returned release function once the verify+record sequence
// finishes. Sharded across 16 mutexes so unrelated usernames do not
// queue on each other.
func (t *LockoutTracker) AttemptLock(username string) func() {
	shard := lockoutShardFor(username)
	t.shards[shard].Lock()
	return t.shards[shard].Unlock
}

// lockoutShardFor returns the shard index for a username via FNV-1a
// 32-bit hash modulo the shard count. FNV is non-cryptographic but
// stable and zero-allocation, which fits a hot path.
func lockoutShardFor(username string) uint32 {
	var hash uint32 = 2166136261
	for i := 0; i < len(username); i++ {
		hash ^= uint32(username[i])
		hash *= 16777619
	}
	return hash % lockoutShardCount
}

// SetStore attaches a persistent backend. Safe to call once at startup
// before any login traffic; subsequent calls replace the backend.
func (t *LockoutTracker) SetStore(store LockoutStore) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.store = store
}

// SetRedactor installs the redaction function used for log fields that
// would otherwise carry raw usernames (R-S-09). Server wires this to
// its HMAC-prefix logUsername so production log aggregators see the
// same correlatable id used elsewhere.
func (t *LockoutTracker) SetRedactor(fn func(string) string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.redactor = fn
}

// redact returns a privacy-preserving identifier for username. Falls
// back to a sha256 prefix when no redactor is wired so unwired callers
// (tests, embedded usage) never leak raw values either.
func (t *LockoutTracker) redact(username string) string {
	t.mu.Lock()
	fn := t.redactor
	t.mu.Unlock()
	if fn != nil {
		return fn(username)
	}
	return defaultRedact(username)
}

// redactLocked is the variant of redact callable while the caller
// already holds t.mu — used by persistLocked / deletePersistedLocked
// in their store-error log paths. Reading t.redactor without
// re-locking is safe because the caller holds t.mu (matched against
// SetRedactor which writes under the same lock). Avoiding the
// re-acquisition prevents a deterministic deadlock when the store
// returns an error (e.g. ctx cancellation) inside a locked critical
// section.
func (t *LockoutTracker) redactLocked(username string) string {
	if t.redactor != nil {
		return t.redactor(username)
	}
	return defaultRedact(username)
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
	lockoutDuration := t.lockoutDurationLocked()
	for _, record := range records {
		entry := lockoutEntry{failures: record.Failures}
		if record.LockedAt != nil {
			if now.Sub(*record.LockedAt) >= lockoutDuration {
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
		slog.Warn("sessions: failed to persist login lockout", "username_hash", t.redactLocked(username), "error", err)
	}
}

func (t *LockoutTracker) deletePersistedLocked(ctx context.Context, username string) {
	if t.store == nil {
		return
	}
	if err := t.store.DeleteLoginLockout(ctx, username); err != nil {
		slog.Warn("sessions: failed to delete login lockout", "username_hash", t.redactLocked(username), "error", err)
	}
}

// IsLockedWithContext
func (t *LockoutTracker) IsLockedWithContext(ctx context.Context, username string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.accounts[username]
	if !ok {
		return false
	}
	if entry.failures < t.maxAttemptsLocked() {
		return false
	}
	if now.Sub(entry.lockedAt) >= t.lockoutDurationLocked() {
		delete(t.accounts, username)
		t.deletePersistedLocked(ctx, username)
		return false
	}
	return true
}

// RecordFailureWithContext
func (t *LockoutTracker) RecordFailureWithContext(ctx context.Context, username string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	maxAttempts := t.maxAttemptsLocked()
	entry := t.accounts[username]
	entry.failures++
	if entry.failures >= maxAttempts {
		entry.lockedAt = now
	}
	t.accounts[username] = entry
	t.persistLocked(ctx, username, entry)

	t.cleanupLocked(ctx, now)
}

// CheckAndRecordFailure atomically checks lockout and records a failure.
// Returns true if the account is locked (failure is NOT recorded when locked).
//
// Note: prefer CheckAndRecordFailureWithContext from request handlers.
func (t *LockoutTracker) CheckAndRecordFailure(username string, now time.Time) bool {
	return t.CheckAndRecordFailureWithContext(context.Background(), username, now)
}

// CheckAndRecordFailureWithContext is the ctx-aware variant of
// CheckAndRecordFailure.
func (t *LockoutTracker) CheckAndRecordFailureWithContext(ctx context.Context, username string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	maxAttempts := t.maxAttemptsLocked()
	lockoutDuration := t.lockoutDurationLocked()
	entry, ok := t.accounts[username]
	if ok && entry.failures >= maxAttempts {
		if now.Sub(entry.lockedAt) < lockoutDuration {
			return true
		}
		entry = lockoutEntry{}
	}

	entry.failures++
	if entry.failures >= maxAttempts {
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

	maxAttempts := t.maxAttemptsLocked()
	lockoutDuration := t.lockoutDurationLocked()
	count := 0
	for _, entry := range t.accounts {
		if entry.failures < maxAttempts {
			continue
		}
		if now.Sub(entry.lockedAt) >= lockoutDuration {
			continue
		}
		count++
	}
	return count
}

// RecordSuccess clears the failure counter after a successful login.
//
// Note: prefer RecordSuccessWithContext from request handlers.
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
	maxAttempts := t.maxAttemptsLocked()
	lockoutDuration := t.lockoutDurationLocked()
	for username, entry := range t.accounts {
		if entry.failures >= maxAttempts && now.Sub(entry.lockedAt) >= lockoutDuration {
			delete(t.accounts, username)
			t.deletePersistedLocked(ctx, username)
		}
	}
}
