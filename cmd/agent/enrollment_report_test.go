package main

import (
	"context"
	"testing"
)

// TestEnrollmentReporterDisableClearsAttempt asserts that Disable() clears the
// bound attempt id and the event buffer so subsequent Record calls become
// no-ops and Flush ships nothing. This guards Issue B: enrollment is
// one-shot, so a reporter that has already flushed once must not pollute the
// same enrollment_attempts row when later reconnect cycles call Record/Flush.
func TestEnrollmentReporterDisableClearsAttempt(t *testing.T) {
	r := newEnrollmentReporter()
	r.Bind("attempt-1")
	r.Record("step1", "info", "msg", nil)
	r.Disable()

	// After Disable, Record should be a no-op: no events buffered and no
	// attempt id bound. We probe internal state via Flush with a nil client:
	// Flush short-circuits when client is nil OR attemptID is empty OR
	// the buffer is empty — none of which should attempt any RPC.
	r.Record("step2", "info", "msg", nil)

	// Flush against a nil client should not panic and should return nil
	// because there is no attempt id and no events to ship.
	if err := r.Flush(context.Background(), nil); err != nil {
		t.Fatalf("Flush after Disable returned err: %v", err)
	}

	// Re-binding after Disable should still work (fresh enrollment cycle).
	r.Bind("attempt-2")
	r.Record("step3", "info", "msg", nil)
	r.mu.Lock()
	bufLen := len(r.events)
	gotAttempt := r.attemptID
	r.mu.Unlock()
	if bufLen != 1 {
		t.Fatalf("after rebind: expected 1 buffered event, got %d", bufLen)
	}
	if gotAttempt != "attempt-2" {
		t.Fatalf("after rebind: expected attempt-2, got %q", gotAttempt)
	}
}

// TestEnrollmentReporterDisableNilSafe asserts Disable() on a nil receiver is
// safe — mirrors the nil-safety contract of Bind/Record/Flush so call sites
// do not need to nil-check the reporter pointer.
func TestEnrollmentReporterDisableNilSafe(t *testing.T) {
	var r *enrollmentReporter
	r.Disable() // must not panic
}
