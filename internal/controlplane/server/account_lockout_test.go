package server

import (
	"testing"
	"time"
)

func TestAccountLockoutNotLockedInitially(t *testing.T) {
	tracker := newAccountLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	if tracker.IsLocked("alice", now) {
		t.Fatal("IsLocked() = true for unknown user, want false")
	}
}

func TestAccountLockoutLocksAfterMaxAttempts(t *testing.T) {
	tracker := newAccountLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < accountLockoutMaxAttempts; i++ {
		tracker.RecordFailure("alice", now)
	}

	if !tracker.IsLocked("alice", now) {
		t.Fatal("IsLocked() = false after max attempts, want true")
	}
}

func TestAccountLockoutUnlocksAfterDuration(t *testing.T) {
	tracker := newAccountLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < accountLockoutMaxAttempts; i++ {
		tracker.RecordFailure("alice", now)
	}

	afterLockout := now.Add(accountLockoutDuration)
	if tracker.IsLocked("alice", afterLockout) {
		t.Fatal("IsLocked() = true after lockout duration expired, want false")
	}
}

func TestAccountLockoutNotLockedBelowThreshold(t *testing.T) {
	tracker := newAccountLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < accountLockoutMaxAttempts-1; i++ {
		tracker.RecordFailure("alice", now)
	}

	if tracker.IsLocked("alice", now) {
		t.Fatal("IsLocked() = true below threshold, want false")
	}
}

func TestAccountLockoutRecordSuccessClearsFailures(t *testing.T) {
	tracker := newAccountLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < accountLockoutMaxAttempts-1; i++ {
		tracker.RecordFailure("alice", now)
	}

	tracker.RecordSuccess("alice")
	tracker.RecordFailure("alice", now)

	if tracker.IsLocked("alice", now) {
		t.Fatal("IsLocked() = true after success reset + 1 failure, want false")
	}
}

func TestAccountLockoutCheckAndRecordFailureAtomic(t *testing.T) {
	tracker := newAccountLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	// Record failures up to threshold via CheckAndRecordFailure.
	for i := 0; i < accountLockoutMaxAttempts-1; i++ {
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

func TestAccountLockoutCheckAndRecordFailureResetsAfterExpiry(t *testing.T) {
	tracker := newAccountLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < accountLockoutMaxAttempts; i++ {
		tracker.CheckAndRecordFailure("alice", now)
	}

	afterExpiry := now.Add(accountLockoutDuration)
	locked := tracker.CheckAndRecordFailure("alice", afterExpiry)
	if locked {
		t.Fatal("CheckAndRecordFailure() = true after expiry, want false (counter reset)")
	}
}

func TestAccountLockoutIsolatesUsers(t *testing.T) {
	tracker := newAccountLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < accountLockoutMaxAttempts; i++ {
		tracker.RecordFailure("alice", now)
	}

	if tracker.IsLocked("bob", now) {
		t.Fatal("IsLocked(bob) = true, want false — lockout should be per-user")
	}
}

func TestAccountLockoutCleanupExpiredEntries(t *testing.T) {
	tracker := newAccountLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	// Fill enough entries to trigger cleanup (threshold is 64).
	for i := 0; i < 70; i++ {
		username := "user" + string(rune('A'+i))
		for j := 0; j < accountLockoutMaxAttempts; j++ {
			tracker.RecordFailure(username, now)
		}
	}

	// Record a new failure well after lockout duration — triggers cleanup.
	future := now.Add(accountLockoutDuration + time.Minute)
	tracker.RecordFailure("trigger-cleanup", future)

	tracker.mu.Lock()
	count := len(tracker.accounts)
	tracker.mu.Unlock()

	// All 70 expired entries should be cleaned, leaving only "trigger-cleanup".
	if count != 1 {
		t.Fatalf("accounts count after cleanup = %d, want 1", count)
	}
}
