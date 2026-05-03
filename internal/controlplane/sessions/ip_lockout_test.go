package sessions

import (
	"testing"
	"time"
)

func TestIPLockoutTracker_NotLockedOnFreshKey(t *testing.T) {
	t.Parallel()
	tr := NewIPLockoutTracker()
	if tr.IsLocked("203.0.113.1", time.Now()) {
		t.Fatal("fresh IP should not be locked")
	}
}

func TestIPLockoutTracker_LocksAfterBudget(t *testing.T) {
	t.Parallel()
	tr := NewIPLockoutTracker()
	now := time.Date(2026, time.May, 3, 0, 0, 0, 0, time.UTC)

	// Drive the counter to one short of the budget — must NOT lock.
	for i := 0; i < IPLockoutMaxFailures-1; i++ {
		tr.RecordFailure("203.0.113.1", now.Add(time.Duration(i)*time.Second))
	}
	if tr.IsLocked("203.0.113.1", now.Add(time.Minute)) {
		t.Fatalf("IP locked at %d failures, want > budget", IPLockoutMaxFailures-1)
	}
	// One more — must lock.
	tr.RecordFailure("203.0.113.1", now.Add(time.Minute))
	if !tr.IsLocked("203.0.113.1", now.Add(time.Minute)) {
		t.Fatalf("IP not locked after %d failures", IPLockoutMaxFailures)
	}
}

func TestIPLockoutTracker_LockedIPRejected(t *testing.T) {
	t.Parallel()
	tr := NewIPLockoutTracker()
	now := time.Date(2026, time.May, 3, 0, 0, 0, 0, time.UTC)

	for i := 0; i < IPLockoutMaxFailures; i++ {
		tr.RecordFailure("203.0.113.1", now.Add(time.Duration(i)*time.Second))
	}
	// 51st attempt via CheckAndRecordFailure must report "already locked".
	// Check inside the lockout window (last failure at ~now+50s plus
	// IPLockoutDuration = ~now+30:50 deadline). One minute later is
	// safely inside.
	locked := tr.CheckAndRecordFailure("203.0.113.1", now.Add(time.Minute))
	if !locked {
		t.Fatalf("CheckAndRecordFailure on locked IP returned false")
	}
}

func TestIPLockoutTracker_DifferentIPsIndependent(t *testing.T) {
	t.Parallel()
	tr := NewIPLockoutTracker()
	now := time.Date(2026, time.May, 3, 0, 0, 0, 0, time.UTC)

	for i := 0; i < IPLockoutMaxFailures; i++ {
		tr.RecordFailure("203.0.113.1", now.Add(time.Duration(i)*time.Second))
	}
	// A second IP must remain unlocked even after the first hit the cap.
	if tr.IsLocked("198.51.100.1", now.Add(time.Minute)) {
		t.Fatal("second IP should not be locked when first hits its own budget")
	}
}

func TestIPLockoutTracker_LockoutExpiresAfterDuration(t *testing.T) {
	t.Parallel()
	tr := NewIPLockoutTracker()
	now := time.Date(2026, time.May, 3, 0, 0, 0, 0, time.UTC)

	for i := 0; i < IPLockoutMaxFailures; i++ {
		tr.RecordFailure("203.0.113.1", now.Add(time.Duration(i)*time.Second))
	}
	// Last failure at now+(max-1)s; lockout deadline is that + duration.
	// Probe at "deadline - 1s" → must remain locked.
	lastFail := now.Add(time.Duration(IPLockoutMaxFailures-1) * time.Second)
	if !tr.IsLocked("203.0.113.1", lastFail.Add(IPLockoutDuration-time.Second)) {
		t.Fatalf("IP must remain locked within IPLockoutDuration")
	}
	// Probe at "deadline + 1s" → fresh budget.
	if tr.IsLocked("203.0.113.1", lastFail.Add(IPLockoutDuration+time.Second)) {
		t.Fatalf("IP must be unlocked after IPLockoutDuration elapses")
	}
}

func TestIPLockoutTracker_OldFailuresDropOutOfWindow(t *testing.T) {
	t.Parallel()
	tr := NewIPLockoutTracker()
	now := time.Date(2026, time.May, 3, 0, 0, 0, 0, time.UTC)

	// Record IPLockoutMaxFailures - 1 in the past, outside the window.
	for i := 0; i < IPLockoutMaxFailures-1; i++ {
		tr.RecordFailure("203.0.113.1", now)
	}
	// Far enough in the future that all prior stamps are pruned.
	future := now.Add(IPLockoutWindow + time.Minute)
	tr.RecordFailure("203.0.113.1", future)
	// Only one failure within the window now — must NOT lock.
	if tr.IsLocked("203.0.113.1", future) {
		t.Fatalf("IP locked even though prior failures are outside the rolling window")
	}
}

func TestIPLockoutTracker_LockedAttemptsDoNotExtendDeadline(t *testing.T) {
	t.Parallel()
	tr := NewIPLockoutTracker()
	now := time.Date(2026, time.May, 3, 0, 0, 0, 0, time.UTC)

	for i := 0; i < IPLockoutMaxFailures; i++ {
		tr.RecordFailure("203.0.113.1", now.Add(time.Duration(i)*time.Second))
	}
	// The deadline is set at the trip moment (last RecordFailure call).
	// Hammer further while locked — those failures must not push the
	// deadline forward.
	tripAt := now.Add(time.Duration(IPLockoutMaxFailures-1) * time.Second)
	deadline := tripAt.Add(IPLockoutDuration)
	for i := 0; i < 200; i++ {
		tr.RecordFailure("203.0.113.1", tripAt.Add(time.Minute+time.Duration(i)*time.Second))
	}
	// One minute past the deadline — must be unlocked.
	if tr.IsLocked("203.0.113.1", deadline.Add(time.Minute)) {
		t.Fatalf("locked-period failures extended the deadline; expected unlocked at %v", deadline.Add(time.Minute))
	}
}

func TestIPLockoutTracker_EmptyKeyNoOp(t *testing.T) {
	t.Parallel()
	tr := NewIPLockoutTracker()
	now := time.Date(2026, time.May, 3, 0, 0, 0, 0, time.UTC)
	tr.RecordFailure("", now)
	if tr.IsLocked("", now) {
		t.Fatal("empty key must not be locked")
	}
}

func TestIPLockoutTracker_ActiveCount(t *testing.T) {
	t.Parallel()
	tr := NewIPLockoutTracker()
	now := time.Date(2026, time.May, 3, 0, 0, 0, 0, time.UTC)

	for _, ip := range []string{"203.0.113.1", "203.0.113.2"} {
		for i := 0; i < IPLockoutMaxFailures; i++ {
			tr.RecordFailure(ip, now.Add(time.Duration(i)*time.Second))
		}
	}
	if got := tr.ActiveCount(now.Add(time.Minute)); got != 2 {
		t.Fatalf("ActiveCount = %d, want 2", got)
	}
}
