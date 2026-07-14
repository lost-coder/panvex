package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// TestExpiryHookReceivesExpiredTargets verifies the batch expiry hook: when a
// TTL sweep flips a job's still-active targets to expired, the registered hook
// is invoked once with every (job, agentID) pair that flipped in that sweep,
// carrying the job's Action and PayloadJSON so the consumer can route it.
func TestExpiryHookReceivesExpiredTargets(t *testing.T) {
	start := time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	currentNow := start
	svc := jobs.NewService()
	svc.SetNow(func() time.Time { return currentNow })

	var batches [][]jobs.ExpiredTarget
	svc.SetExpiryHook(func(expired []jobs.ExpiredTarget) {
		batches = append(batches, expired)
	})

	if _, err := svc.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1", "agent-2"},
		TTL:            time.Minute,
		IdempotencyKey: "expire-key",
		ActorID:        "user-1",
		PayloadJSON:    `{"client_id":"c1"}`,
	}, currentNow); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Pre-TTL: the read-path sweep is a no-op, so the hook stays silent.
	currentNow = start.Add(30 * time.Second)
	svc.ListWithContext(context.Background())
	if len(batches) != 0 {
		t.Fatalf("hook fired before TTL: %v", batches)
	}

	// Post-TTL: one sweep flips both targets -> exactly one hook batch of two.
	currentNow = start.Add(2 * time.Minute)
	svc.ListWithContext(context.Background())

	if len(batches) != 1 {
		t.Fatalf("hook batches = %d, want 1", len(batches))
	}
	batch := batches[0]
	if len(batch) != 2 {
		t.Fatalf("batch size = %d, want 2", len(batch))
	}
	seen := map[string]bool{}
	for _, target := range batch {
		seen[target.AgentID] = true
		if target.Job.Action != jobs.ActionRuntimeReload {
			t.Fatalf("target.Job.Action = %q, want %q", target.Job.Action, jobs.ActionRuntimeReload)
		}
		if target.Job.PayloadJSON != `{"client_id":"c1"}` {
			t.Fatalf("target.Job.PayloadJSON = %q", target.Job.PayloadJSON)
		}
	}
	if !seen["agent-1"] || !seen["agent-2"] {
		t.Fatalf("expired agents = %v, want agent-1 and agent-2", seen)
	}

	// A second post-TTL sweep must not re-fire: the targets are already expired.
	svc.ListWithContext(context.Background())
	if len(batches) != 1 {
		t.Fatalf("hook re-fired on an already-expired job: %d batches", len(batches))
	}
}

// TestExpiryHookNotInvokedUnderLock is the deadlock canary. The hook re-enters
// the service via Get, which takes the read lock. If the sweep invoked the hook
// while still holding the write lock, Get would deadlock; the watchdog turns
// that hang into a test failure instead of a stuck run.
func TestExpiryHookNotInvokedUnderLock(t *testing.T) {
	start := time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	currentNow := start
	svc := jobs.NewService()
	svc.SetNow(func() time.Time { return currentNow })

	reentered := make(chan int, 1)
	svc.SetExpiryHook(func(expired []jobs.ExpiredTarget) {
		// Call back into the service under the same mutex family.
		for _, target := range expired {
			_, _ = svc.Get(target.Job.ID)
		}
		reentered <- len(expired)
	})

	if _, err := svc.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "expire-key",
		ActorID:        "user-1",
	}, currentNow); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	currentNow = start.Add(2 * time.Minute)
	done := make(chan struct{})
	go func() {
		svc.ListWithContext(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ListWithContext deadlocked: expiry hook was invoked while holding the service lock")
	}

	select {
	case n := <-reentered:
		if n != 1 {
			t.Fatalf("hook received %d expired targets, want 1", n)
		}
	default:
		t.Fatal("expiry hook did not run")
	}
}
