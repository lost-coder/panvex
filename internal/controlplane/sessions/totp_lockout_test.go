package sessions

import (
	"context"
	"testing"
	"time"
)

// S-6: a fresh tracker reports nobody locked.
func TestTOTPLockoutNotLockedInitially(t *testing.T) {
	tracker := NewTOTPLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	if tracker.IsLocked("alice", now) {
		t.Fatal("IsLocked() = true for unknown user, want false")
	}
}

// S-6: 3 wrong TOTP codes trip the counter; the 4th attempt must see a
// locked account.
func TestTOTPLockoutAfterMaxAttempts(t *testing.T) {
	tracker := NewTOTPLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	// Three failures = max; tracker records lockedAt on the third one.
	for i := 0; i < TOTPLockoutMaxAttempts; i++ {
		tracker.RecordFailure("alice", now)
	}

	if !tracker.IsLocked("alice", now) {
		t.Fatalf("IsLocked() after %d failures = false, want true", TOTPLockoutMaxAttempts)
	}
}

// S-6: 2 failures must NOT lock — only the threshold-th does.
func TestTOTPLockoutNotLockedBelowThreshold(t *testing.T) {
	tracker := NewTOTPLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < TOTPLockoutMaxAttempts-1; i++ {
		tracker.RecordFailure("alice", now)
	}

	if tracker.IsLocked("alice", now) {
		t.Fatal("IsLocked() = true below threshold, want false")
	}
}

// S-6: the lockout window auto-releases after TOTPLockoutDuration.
// Uses a mock clock — a t.Cleanup hook resets a captured timestamp so
// nothing leaks between tests if they share state in the future.
func TestTOTPLockoutExpiresAfterDuration(t *testing.T) {
	tracker := NewTOTPLockoutTracker()
	mockNow := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		// Mock-clock reset: zero the fake "now" so a follow-up test
		// reusing this pattern starts from a clean slate.
		mockNow = time.Time{}
	})

	for i := 0; i < TOTPLockoutMaxAttempts; i++ {
		tracker.RecordFailure("alice", mockNow)
	}

	afterLockout := mockNow.Add(TOTPLockoutDuration)
	if tracker.IsLocked("alice", afterLockout) {
		t.Fatalf("IsLocked() after %s = true, want false", TOTPLockoutDuration)
	}
}

// S-6: a successful TOTP must immediately clear the counter so a user
// who fat-fingered once is not stuck.
func TestTOTPLockoutRecordSuccessClearsFailures(t *testing.T) {
	tracker := NewTOTPLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	tracker.RecordFailure("alice", now)
	tracker.RecordFailure("alice", now)
	tracker.RecordSuccess("alice")

	// After RecordSuccess, the counter is fully reset: another two
	// failures must still leave the account unlocked.
	tracker.RecordFailure("alice", now)
	tracker.RecordFailure("alice", now)
	if tracker.IsLocked("alice", now) {
		t.Fatal("IsLocked() = true after RecordSuccess + 2 failures, want false")
	}
}

// S-6: lockout is per-user.
func TestTOTPLockoutIsPerUser(t *testing.T) {
	tracker := NewTOTPLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < TOTPLockoutMaxAttempts; i++ {
		tracker.RecordFailure("alice", now)
	}

	if tracker.IsLocked("bob", now) {
		t.Fatal("IsLocked(bob) = true, want false — TOTP lockout should be per-user")
	}
}

// S-6: CheckAndRecordFailure returns true once the account is locked
// and stops incrementing past the threshold.
func TestTOTPLockoutCheckAndRecordFailure(t *testing.T) {
	tracker := NewTOTPLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < TOTPLockoutMaxAttempts-1; i++ {
		if locked := tracker.CheckAndRecordFailure("alice", now); locked {
			t.Fatalf("CheckAndRecordFailure() = true on attempt %d, want false", i+1)
		}
	}

	// Threshold-th call: still false because the account was not locked
	// before this call, but the call itself trips the counter.
	if locked := tracker.CheckAndRecordFailure("alice", now); locked {
		t.Fatal("CheckAndRecordFailure() = true on triggering attempt, want false")
	}

	// All subsequent calls within the window must report locked.
	if locked := tracker.CheckAndRecordFailure("alice", now); !locked {
		t.Fatal("CheckAndRecordFailure() = false on locked account, want true")
	}
}

// S-6: after the window passes, CheckAndRecordFailure resets and the
// account is treated as unlocked again.
func TestTOTPLockoutCheckAndRecordFailureResetsAfterExpiry(t *testing.T) {
	tracker := NewTOTPLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < TOTPLockoutMaxAttempts; i++ {
		tracker.CheckAndRecordFailure("alice", now)
	}

	future := now.Add(TOTPLockoutDuration + time.Second)
	if locked := tracker.CheckAndRecordFailure("alice", future); locked {
		t.Fatal("CheckAndRecordFailure() after expiry = true, want false")
	}
}

// S-6: ActiveCount tracks how many users are currently TOTP-locked.
func TestTOTPLockoutActiveCount(t *testing.T) {
	tracker := NewTOTPLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	if got := tracker.ActiveCount(now); got != 0 {
		t.Fatalf("ActiveCount() = %d, want 0", got)
	}
	for i := 0; i < TOTPLockoutMaxAttempts; i++ {
		tracker.RecordFailure("alice", now)
	}
	if got := tracker.ActiveCount(now); got != 1 {
		t.Fatalf("ActiveCount() = %d, want 1", got)
	}
	future := now.Add(TOTPLockoutDuration + time.Second)
	if got := tracker.ActiveCount(future); got != 0 {
		t.Fatalf("ActiveCount() after expiry = %d, want 0", got)
	}
}

// S-6: the WithContext variants accept a ctx parameter for API symmetry
// with LockoutTracker. They must behave identically to the non-ctx
// variants.
func TestTOTPLockoutWithContextAPI(t *testing.T) {
	tracker := NewTOTPLockoutTracker()
	ctx := context.Background()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	for i := 0; i < TOTPLockoutMaxAttempts; i++ {
		tracker.RecordFailureWithContext(ctx, "alice", now)
	}
	if !tracker.IsLockedWithContext(ctx, "alice", now) {
		t.Fatal("IsLockedWithContext after failures = false, want true")
	}
	tracker.RecordSuccessWithContext(ctx, "alice")
	if tracker.IsLockedWithContext(ctx, "alice", now) {
		t.Fatal("IsLockedWithContext after RecordSuccessWithContext = true, want false")
	}
}

// S-6: the password counter and the TOTP counter are independent. A
// password tracker tripped to its own threshold must not lock the
// TOTP path, and vice versa. This is the core property the audit
// finding asks for: an attacker holding a valid password cannot keep
// burning TOTP guesses against the lenient password budget.
func TestTOTPLockoutIndependentFromPasswordLockout(t *testing.T) {
	password := NewLockoutTracker()
	totp := NewTOTPLockoutTracker()
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)

	// Trip the password tracker hard. The TOTP tracker must still be
	// at zero (different counter, different storage).
	for i := 0; i < LockoutMaxAttempts; i++ {
		password.RecordFailure("alice", now)
	}
	if !password.IsLocked("alice", now) {
		t.Fatal("precondition: password tracker should be locked")
	}
	if totp.IsLocked("alice", now) {
		t.Fatal("TOTP tracker locked by password failures, want independent counters")
	}

	// Reset and go the other way: TOTP-trip must not affect the
	// password tracker.
	password2 := NewLockoutTracker()
	totp2 := NewTOTPLockoutTracker()
	for i := 0; i < TOTPLockoutMaxAttempts; i++ {
		totp2.RecordFailure("alice", now)
	}
	if !totp2.IsLocked("alice", now) {
		t.Fatal("precondition: totp tracker should be locked")
	}
	if password2.IsLocked("alice", now) {
		t.Fatal("password tracker locked by TOTP failures, want independent counters")
	}
}

// S-6: TOTP duration (5 min) is strictly shorter than password duration
// (15 min), and TOTP threshold (3) is strictly lower than password
// threshold (5). Lock these via the constants so a future tweak that
// inverts the ordering trips a compile-time-adjacent test failure.
func TestTOTPLockoutThresholdsAreStricter(t *testing.T) {
	if TOTPLockoutMaxAttempts >= LockoutMaxAttempts {
		t.Fatalf("TOTPLockoutMaxAttempts (%d) must be < LockoutMaxAttempts (%d)",
			TOTPLockoutMaxAttempts, LockoutMaxAttempts)
	}
	if TOTPLockoutDuration >= LockoutDuration {
		t.Fatalf("TOTPLockoutDuration (%s) must be < LockoutDuration (%s)",
			TOTPLockoutDuration, LockoutDuration)
	}
}
