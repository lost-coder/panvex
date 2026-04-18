package sessions

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeLockoutStore is an in-memory LockoutStore for unit tests.
// It is deliberately simple — the contract tests that exercise the
// real SQLite/PostgreSQL stores live in storage/storagetest.
type fakeLockoutStore struct {
	records map[string]LockoutRecord
}

func newFakeLockoutStore() *fakeLockoutStore {
	return &fakeLockoutStore{records: make(map[string]LockoutRecord)}
}

func (f *fakeLockoutStore) UpsertLoginLockout(_ context.Context, record LockoutRecord) error {
	f.records[record.Username] = record
	return nil
}

func (f *fakeLockoutStore) GetLoginLockout(_ context.Context, username string) (LockoutRecord, error) {
	record, ok := f.records[username]
	if !ok {
		return LockoutRecord{}, errors.New("not found")
	}
	return record, nil
}

func (f *fakeLockoutStore) DeleteLoginLockout(_ context.Context, username string) error {
	delete(f.records, username)
	return nil
}

func (f *fakeLockoutStore) ListLoginLockouts(_ context.Context) ([]LockoutRecord, error) {
	out := make([]LockoutRecord, 0, len(f.records))
	for _, r := range f.records {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeLockoutStore) DeleteExpiredLoginLockouts(_ context.Context, before time.Time) (int64, error) {
	var n int64
	for username, record := range f.records {
		if record.UpdatedAt.Before(before) {
			delete(f.records, username)
			n++
		}
	}
	return n, nil
}

func TestLockoutNotLockedInitially(t *testing.T) {
	tracker := NewLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	if tracker.IsLocked("alice", now) {
		t.Fatal("IsLocked() = true for unknown user, want false")
	}
}

func TestLockoutAfterMaxAttempts(t *testing.T) {
	tracker := NewLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < LockoutMaxAttempts; i++ {
		tracker.RecordFailure("alice", now)
	}

	if !tracker.IsLocked("alice", now) {
		t.Fatal("IsLocked() = false after MaxAttempts failures, want true")
	}
}

func TestLockoutExpiresAfterDuration(t *testing.T) {
	tracker := NewLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < LockoutMaxAttempts; i++ {
		tracker.RecordFailure("alice", now)
	}

	afterLockout := now.Add(LockoutDuration)
	if tracker.IsLocked("alice", afterLockout) {
		t.Fatal("IsLocked() = true after lockout duration expired, want false")
	}
}

func TestLockoutNotLockedBelowThreshold(t *testing.T) {
	tracker := NewLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < LockoutMaxAttempts-1; i++ {
		tracker.RecordFailure("alice", now)
	}

	if tracker.IsLocked("alice", now) {
		t.Fatal("IsLocked() = true below threshold, want false")
	}
}

func TestLockoutCheckAndRecordFailure(t *testing.T) {
	tracker := NewLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	// Record failures up to threshold via CheckAndRecordFailure.
	for i := 0; i < LockoutMaxAttempts-1; i++ {
		locked := tracker.CheckAndRecordFailure("alice", now)
		if locked {
			t.Fatalf("CheckAndRecordFailure() = true on attempt %d, want false", i+1)
		}
	}

	// This attempt should trigger lockout (5th failure) but still return false
	// because the account wasn't locked *before* this call.
	locked := tracker.CheckAndRecordFailure("alice", now)
	if locked {
		t.Fatal("CheckAndRecordFailure() = true on triggering attempt, want false")
	}

	// Now account is locked — next call should return true and NOT record.
	locked = tracker.CheckAndRecordFailure("alice", now)
	if !locked {
		t.Fatal("CheckAndRecordFailure() = false on locked account, want true")
	}
}

func TestLockoutCheckAndRecordFailureResetsAfterExpiry(t *testing.T) {
	tracker := NewLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < LockoutMaxAttempts; i++ {
		tracker.CheckAndRecordFailure("alice", now)
	}

	future := now.Add(LockoutDuration + time.Second)
	locked := tracker.CheckAndRecordFailure("alice", future)
	if locked {
		t.Fatal("CheckAndRecordFailure() after expiry = true, want false")
	}
}

func TestLockoutRecordSuccessClearsFailures(t *testing.T) {
	tracker := NewLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < LockoutMaxAttempts-1; i++ {
		tracker.RecordFailure("alice", now)
	}
	tracker.RecordSuccess("alice")
	for i := 0; i < LockoutMaxAttempts-1; i++ {
		if tracker.IsLocked("alice", now) {
			t.Fatalf("IsLocked() after RecordSuccess + %d failures = true, want false", i+1)
		}
		tracker.RecordFailure("alice", now)
	}
}

func TestLockoutIsPerUser(t *testing.T) {
	tracker := NewLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < LockoutMaxAttempts; i++ {
		tracker.RecordFailure("alice", now)
	}

	if tracker.IsLocked("bob", now) {
		t.Fatal("IsLocked(bob) = true, want false — lockout should be per-user")
	}
}

func TestLockoutActiveCount(t *testing.T) {
	tracker := NewLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	if got := tracker.ActiveCount(now); got != 0 {
		t.Fatalf("ActiveCount() = %d, want 0", got)
	}
	for i := 0; i < LockoutMaxAttempts; i++ {
		tracker.RecordFailure("alice", now)
	}
	if got := tracker.ActiveCount(now); got != 1 {
		t.Fatalf("ActiveCount() = %d, want 1", got)
	}
	// After expiry, active count drops back to zero.
	future := now.Add(LockoutDuration + time.Second)
	if got := tracker.ActiveCount(future); got != 0 {
		t.Fatalf("ActiveCount() after expiry = %d, want 0", got)
	}
}

func TestLockoutCleanupExpiredEntries(t *testing.T) {
	tracker := NewLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	// Fill enough entries to trigger cleanup (threshold is 64).
	for i := 0; i < 70; i++ {
		username := "user" + string(rune('A'+i))
		for j := 0; j < LockoutMaxAttempts; j++ {
			tracker.RecordFailure(username, now)
		}
	}

	// Record a new failure well after lockout duration — triggers cleanup.
	future := now.Add(LockoutDuration + time.Minute)
	tracker.RecordFailure("trigger-cleanup", future)

	tracker.mu.Lock()
	count := len(tracker.accounts)
	tracker.mu.Unlock()

	// All 70 expired entries should be cleaned, leaving only "trigger-cleanup".
	if count != 1 {
		t.Fatalf("accounts count after cleanup = %d, want 1", count)
	}
}

// S7: failures survive a simulated process restart. A fresh tracker
// restoring from the same LockoutStore must see the same failure count
// as the tracker that recorded the failures originally.
func TestLockoutPersistsAcrossRestart(t *testing.T) {
	store := newFakeLockoutStore()
	now := time.Date(2026, time.April, 19, 10, 0, 0, 0, time.UTC)

	first := NewLockoutTracker()
	first.SetStore(store)
	for i := 0; i < LockoutMaxAttempts; i++ {
		first.RecordFailure("alice", now.Add(time.Duration(i)*time.Second))
	}
	if !first.IsLocked("alice", now.Add(10*time.Second)) {
		t.Fatal("precondition: first tracker should be locked")
	}

	// Simulate restart: brand-new tracker pointed at the same store.
	second := NewLockoutTracker()
	second.SetStore(store)
	if err := second.Restore(context.Background(), now.Add(10*time.Second)); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if !second.IsLocked("alice", now.Add(10*time.Second)) {
		t.Fatal("second tracker did not inherit lockout after Restore()")
	}
}

// S7: a successful login through the tracker must delete the record
// from the store so a later restart sees a clean account.
func TestLockoutRecordSuccessPurgesStoredRow(t *testing.T) {
	store := newFakeLockoutStore()
	tracker := NewLockoutTracker()
	tracker.SetStore(store)
	now := time.Date(2026, time.April, 19, 10, 0, 0, 0, time.UTC)

	tracker.RecordFailure("bob", now)
	if _, ok := store.records["bob"]; !ok {
		t.Fatal("RecordFailure did not persist")
	}

	tracker.RecordSuccess("bob")
	if _, ok := store.records["bob"]; ok {
		t.Fatal("RecordSuccess did not purge persisted row")
	}
}

// S7: records whose lockout window has already elapsed at restart
// time must NOT revive the lockout; they were expiring anyway.
func TestLockoutRestoreSkipsExpiredLockouts(t *testing.T) {
	store := newFakeLockoutStore()
	lockedAt := time.Date(2026, time.April, 19, 10, 0, 0, 0, time.UTC)
	lockedAtCopy := lockedAt
	store.records["ghost"] = LockoutRecord{
		Username:  "ghost",
		Failures:  LockoutMaxAttempts,
		LockedAt:  &lockedAtCopy,
		UpdatedAt: lockedAt,
	}

	tracker := NewLockoutTracker()
	tracker.SetStore(store)
	future := lockedAt.Add(LockoutDuration + time.Minute)
	if err := tracker.Restore(context.Background(), future); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if tracker.IsLocked("ghost", future) {
		t.Fatal("expired lockout resurrected by Restore")
	}
}
