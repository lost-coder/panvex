package sessions

import (
	"testing"
	"time"
)

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
