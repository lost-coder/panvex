package jobs

// White-box tests for jobs.Service that require direct access to unexported
// fields (service.mu, service.keys, service.jobs, service.agentJobs).
//
// Tests that only use exported APIs but import storage/sqlite were moved to
// service_integration_test.go (package jobs_test) to break the import cycle
// introduced when storage/sqlite/jobs_repository.go imports package jobs.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestJobsKeysEviction verifies P2-PERF-03: completed jobs have their
// idempotency keys evicted by PruneKeys once older than the TTL, preventing
// unbounded growth of jobs.Service.keys. Keys for jobs still queued or
// running must NOT be evicted.
func TestJobsKeysEviction(t *testing.T) {
	start := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	now := start
	service := NewService()
	service.SetNow(func() time.Time { return now })

	// Job A: will be completed, then aged past TTL — should be evicted.
	jobA, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "key-a",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue(A) error = %v", err)
	}

	// Job B: will stay live the whole test — must never be evicted.
	_, err = service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-2"},
		TTL:            time.Hour,
		IdempotencyKey: "key-b",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue(B) error = %v", err)
	}

	// Complete job A with success. This must record the terminal timestamp.
	service.RecordResult(context.Background(), "agent-1", jobA.ID, true, "ok", "", now)

	// Before advancing time, PruneKeys with a 24h TTL must retain both keys.
	if evicted := service.PruneKeys(24 * time.Hour); evicted != 0 {
		t.Fatalf("PruneKeys immediate = %d, want 0", evicted)
	}
	if _, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "key-a",
		ActorID:        "user-1",
	}, now); !errors.Is(err, ErrDuplicateIdempotencyKey) {
		t.Fatalf("Enqueue(key-a) immediate error = %v, want duplicate", err)
	}

	// Advance past the TTL and prune again — key-a must now be evicted.
	now = start.Add(25 * time.Hour)
	evicted := service.PruneKeys(24 * time.Hour)
	if evicted != 1 {
		t.Fatalf("PruneKeys after TTL = %d, want 1", evicted)
	}

	// key-a should now be re-usable (key has been evicted from the map).
	if _, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "key-a",
		ActorID:        "user-1",
	}, now); err != nil {
		t.Fatalf("Enqueue(key-a) after eviction error = %v, want nil", err)
	}

	// key-b is still live — it must remain protected from eviction.
	if _, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-2"},
		TTL:            time.Hour,
		IdempotencyKey: "key-b",
		ActorID:        "user-1",
	}, now); !errors.Is(err, ErrDuplicateIdempotencyKey) {
		t.Fatalf("Enqueue(key-b) after eviction error = %v, want duplicate", err)
	}
}

// TestJobsKeysEvictionViaExpiredJobs verifies that keys for jobs that reach
// the Expired terminal state (via TTL, not explicit completion) are also
// subject to eviction, matching the behaviour for Succeeded/Failed jobs.
func TestJobsKeysEvictionViaExpiredJobs(t *testing.T) {
	start := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	now := start
	service := NewService()
	service.SetNow(func() time.Time { return now })

	if _, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "key-expire",
		ActorID:        "user-1",
	}, now); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	// Advance past job TTL so ExpireStale() moves it to Expired.
	now = start.Add(2 * time.Minute)
	service.ExpireStale()

	// Now advance past the key eviction TTL.
	now = start.Add(25 * time.Hour)
	if evicted := service.PruneKeys(24 * time.Hour); evicted != 1 {
		t.Fatalf("PruneKeys for expired job = %d, want 1", evicted)
	}

	// Re-enqueue with the same key should succeed.
	if _, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "key-expire",
		ActorID:        "user-1",
	}, now); err != nil {
		t.Fatalf("Enqueue() after eviction error = %v", err)
	}
}

// TestJobsKeysEvictionWorker exercises the StartKeyEvictionWorker ticker:
// it must call PruneKeys periodically and stop cleanly when ctx is
// cancelled. Uses a short interval (10ms) to keep the test fast.
func TestJobsKeysEvictionWorker(t *testing.T) {
	start := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	now := start
	var nowMu sync.Mutex
	nowFn := func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	}
	service := NewService()
	service.SetNow(nowFn)

	jobA, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "key-worker",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	service.RecordResult(context.Background(), "agent-1", jobA.ID, true, "ok", "", now)

	// Age the terminal timestamp past TTL by moving the clock forward.
	nowMu.Lock()
	now = start.Add(48 * time.Hour)
	nowMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	service.StartKeyEvictionWorker(ctx, 10*time.Millisecond, 24*time.Hour, &wg)

	// Poll up to 2s for the worker to run at least once and evict the key.
	deadline := time.Now().Add(2 * time.Second)
	evicted := false
	for time.Now().Before(deadline) {
		service.mu.Lock()
		_, stillPresent := service.keys["key-worker"]
		service.mu.Unlock()
		if !stillPresent {
			evicted = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	wg.Wait()

	if !evicted {
		t.Fatal("StartKeyEvictionWorker did not evict terminal key within 2s")
	}
}

func TestServiceEnqueueRejectsDuplicateIdempotencyKey(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	first, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "same-key",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() first error = %v", err)
	}

	if first.Status != StatusQueued {
		t.Fatalf("first.Status = %q, want %q", first.Status, StatusQueued)
	}

	_, err = service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "same-key",
		ActorID:        "user-1",
	}, now.Add(10*time.Second))
	if err == nil {
		t.Fatal("Enqueue() duplicate error = nil, want idempotency failure")
	}

	if !errors.Is(err, ErrDuplicateIdempotencyKey) {
		t.Fatalf("Enqueue() duplicate error = %v, want %v", err, ErrDuplicateIdempotencyKey)
	}
}

func TestServiceEnqueueRejectsMutatingActionForReadOnlyTarget(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	_, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionUsersCreate,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "create-user",
		ActorID:        "user-1",
		ReadOnlyAgents: map[string]bool{
			"agent-1": true,
		},
	}, now)
	if err == nil {
		t.Fatal("Enqueue() error = nil, want read-only failure")
	}

	if !errors.Is(err, ErrReadOnlyTarget) {
		t.Fatalf("Enqueue() error = %v, want %v", err, ErrReadOnlyTarget)
	}
}

func TestServiceMarkAcknowledgedTransitionsTargetState(t *testing.T) {
	now := time.Date(2026, time.March, 18, 10, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time {
		return now.Add(10 * time.Second)
	})

	job, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "ack-transition",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	service.MarkDelivered(context.Background(), "agent-1", job.ID, now.Add(2*time.Second))
	service.MarkAcknowledged(context.Background(), "agent-1", job.ID, now.Add(3*time.Second))

	list := service.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(list), 1)
	}
	if list[0].Status != StatusRunning {
		t.Fatalf("jobs[0].Status = %q, want %q", list[0].Status, StatusRunning)
	}
	if list[0].Targets[0].Status != TargetStatusAcknowledged {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", list[0].Targets[0].Status, TargetStatusAcknowledged)
	}
}

func TestServiceMarkDeliveredDoesNotDowngradeAcknowledgedTarget(t *testing.T) {
	now := time.Date(2026, time.March, 18, 10, 30, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time {
		return now.Add(10 * time.Second)
	})

	job, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "ack-no-downgrade",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	service.MarkDelivered(context.Background(), "agent-1", job.ID, now.Add(2*time.Second))
	service.MarkAcknowledged(context.Background(), "agent-1", job.ID, now.Add(3*time.Second))
	service.MarkDelivered(context.Background(), "agent-1", job.ID, now.Add(4*time.Second))

	list := service.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(list), 1)
	}
	if list[0].Status != StatusRunning {
		t.Fatalf("jobs[0].Status = %q, want %q", list[0].Status, StatusRunning)
	}
	if list[0].Targets[0].Status != TargetStatusAcknowledged {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", list[0].Targets[0].Status, TargetStatusAcknowledged)
	}
}

func TestServiceMarkAcknowledgedIgnoresQueuedTarget(t *testing.T) {
	now := time.Date(2026, time.March, 18, 10, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time {
		return now.Add(10 * time.Second)
	})

	job, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "ack-queued",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	service.MarkAcknowledged(context.Background(), "agent-1", job.ID, now.Add(5*time.Second))

	list := service.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(list), 1)
	}
	if list[0].Status != StatusQueued {
		t.Fatalf("jobs[0].Status = %q, want %q", list[0].Status, StatusQueued)
	}
	if list[0].Targets[0].Status != TargetStatusQueued {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", list[0].Targets[0].Status, TargetStatusQueued)
	}
}

func TestServicePendingForAgentReturnsQueuedAndStaleSentJobs(t *testing.T) {
	const retryAfter = 30 * time.Second
	start := time.Date(2026, time.March, 19, 9, 0, 0, 0, time.UTC)
	now := start
	service := NewService()
	service.SetNow(func() time.Time {
		return now
	})

	queued, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-queued",
		ActorID:        "user-1",
	}, start.Add(-3*time.Minute))
	if err != nil {
		t.Fatalf("Enqueue(queued) error = %v", err)
	}
	staleSent, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-stale-sent",
		ActorID:        "user-1",
	}, start.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("Enqueue(staleSent) error = %v", err)
	}
	recentSent, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-recent-sent",
		ActorID:        "user-1",
	}, start.Add(-time.Minute))
	if err != nil {
		t.Fatalf("Enqueue(recentSent) error = %v", err)
	}
	otherAgent, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-2"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-other-agent",
		ActorID:        "user-1",
	}, start.Add(-30*time.Second))
	if err != nil {
		t.Fatalf("Enqueue(otherAgent) error = %v", err)
	}

	// D3: staleness is simulated by moving the panel clock — UpdatedAt is
	// stamped with s.now(), the agent-reported observedAt is ignored.
	now = start.Add(-(retryAfter + time.Second))
	service.MarkDelivered(context.Background(), "agent-1", staleSent.ID, now)
	now = start.Add(-(retryAfter - time.Second))
	service.MarkDelivered(context.Background(), "agent-1", recentSent.ID, now)
	now = start

	pending := service.PendingForAgent(context.Background(), "agent-1", retryAfter)
	if len(pending) != 2 {
		t.Fatalf("len(PendingForAgent) = %d, want %d", len(pending), 2)
	}
	if pending[0].ID != queued.ID {
		t.Fatalf("pending[0].ID = %q, want %q", pending[0].ID, queued.ID)
	}
	if pending[1].ID != staleSent.ID {
		t.Fatalf("pending[1].ID = %q, want %q", pending[1].ID, staleSent.ID)
	}
	for _, job := range pending {
		if job.ID == recentSent.ID {
			t.Fatalf("pending contains recent sent job %q, want excluded", recentSent.ID)
		}
		if job.ID == otherAgent.ID {
			t.Fatalf("pending contains other-agent job %q, want excluded", otherAgent.ID)
		}
	}
}

// TestServicePendingForAgentRedeliversAcknowledgedAfterRetryWindow guards
// H-7: an acknowledged target must stay in the per-agent index so that, if
// its JobResult is lost in transit after the ack (backpressure / stream drop
// / agent crash), PendingForAgent re-dispatches it once the retryAfter window
// elapses — instead of the job hanging in "running" until a CP restart or TTL
// expiry. Within the window it must NOT be re-dispatched.
func TestServicePendingForAgentRedeliversAcknowledgedAfterRetryWindow(t *testing.T) {
	const retryAfter = 30 * time.Second
	base := time.Date(2026, time.March, 19, 9, 45, 0, 0, time.UTC)
	clock := base
	service := NewService()
	service.SetNow(func() time.Time { return clock })

	job, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-index-ack-redeliver",
		ActorID:        "user-1",
	}, base)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	service.MarkDelivered(context.Background(), "agent-1", job.ID, base.Add(time.Second))
	service.MarkAcknowledged(context.Background(), "agent-1", job.ID, base.Add(2*time.Second))

	// Within the retry window: not re-dispatched...
	clock = base.Add(10 * time.Second)
	if pending := service.PendingForAgent(context.Background(), "agent-1", retryAfter); len(pending) != 0 {
		t.Fatalf("within retry window len(PendingForAgent) = %d, want 0", len(pending))
	}
	// ...but still indexed so a lost-after-ack result can be retried.
	if _, ok := service.agentJobs["agent-1"][job.ID]; !ok {
		t.Fatal("acknowledged job dropped from index; a result lost after ack could never be retried")
	}

	// After the retry window elapses, it is re-dispatched.
	clock = base.Add(2*time.Second + retryAfter + time.Second)
	if pending := service.PendingForAgent(context.Background(), "agent-1", retryAfter); len(pending) != 1 {
		t.Fatalf("after retry window len(PendingForAgent) = %d, want 1 (redelivery)", len(pending))
	}
}

func TestServiceListProjectsExpiredQueuedJobsAsFailed(t *testing.T) {
	service := NewService()
	now := time.Now().UTC().Add(-2 * time.Minute)

	job, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "expired-job",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	list := service.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(list), 1)
	}
	if list[0].ID != job.ID {
		t.Fatalf("jobs[0].ID = %q, want %q", list[0].ID, job.ID)
	}
	if list[0].Status != StatusExpired {
		t.Fatalf("jobs[0].Status = %q, want %q", list[0].Status, StatusExpired)
	}
	if list[0].Targets[0].Status != TargetStatusExpired {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", list[0].Targets[0].Status, TargetStatusExpired)
	}

	stored := service.jobs[job.ID]
	if stored.Status != StatusExpired {
		t.Fatalf("stored.Status = %q, want %q", stored.Status, StatusExpired)
	}
	if stored.Targets[0].Status != TargetStatusExpired {
		t.Fatalf("stored.Targets[0].Status = %q, want %q", stored.Targets[0].Status, TargetStatusExpired)
	}
}

func TestServiceRecordResultDoesNotOverrideExpiredTarget(t *testing.T) {
	now := time.Date(2026, time.March, 19, 10, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time {
		return now.Add(2 * time.Minute)
	})

	job, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "expired-result",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	service.RecordResult(context.Background(), "agent-1", job.ID, true, "late success", "", now.Add(3*time.Minute))

	list := service.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(list), 1)
	}
	if list[0].Status != StatusExpired {
		t.Fatalf("jobs[0].Status = %q, want %q", list[0].Status, StatusExpired)
	}
	if list[0].Targets[0].Status != TargetStatusExpired {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", list[0].Targets[0].Status, TargetStatusExpired)
	}
	if list[0].Targets[0].ResultText != "" {
		t.Fatalf("jobs[0].Targets[0].ResultText = %q, want empty string", list[0].Targets[0].ResultText)
	}
}

func TestServiceUpdateTargetDoesNotExpireUnrelatedJobs(t *testing.T) {
	baseNow := time.Date(2026, time.March, 26, 11, 0, 0, 0, time.UTC)
	currentTime := baseNow
	service := NewService()
	service.SetNow(func() time.Time {
		return currentTime
	})

	expiredJob, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-expired"},
		TTL:            time.Minute,
		IdempotencyKey: "unrelated-expired",
		ActorID:        "user-1",
	}, baseNow)
	if err != nil {
		t.Fatalf("Enqueue(expired) error = %v", err)
	}

	liveJob, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-live"},
		TTL:            time.Hour,
		IdempotencyKey: "unrelated-live",
		ActorID:        "user-1",
	}, baseNow)
	if err != nil {
		t.Fatalf("Enqueue(live) error = %v", err)
	}

	currentTime = baseNow.Add(2 * time.Minute)
	service.MarkDelivered(context.Background(), "agent-live", liveJob.ID, currentTime)

	storedExpired := service.jobs[expiredJob.ID]
	if storedExpired.Status != StatusQueued {
		t.Fatalf("stored expired job status = %q, want %q before List()", storedExpired.Status, StatusQueued)
	}
	if storedExpired.Targets[0].Status != TargetStatusQueued {
		t.Fatalf("stored expired target status = %q, want %q before List()", storedExpired.Targets[0].Status, TargetStatusQueued)
	}

	jobsSnapshot := service.List()
	for _, listed := range jobsSnapshot {
		if listed.ID != expiredJob.ID {
			continue
		}
		if listed.Status != StatusExpired {
			t.Fatalf("listed expired job status = %q, want %q", listed.Status, StatusExpired)
		}
		if listed.Targets[0].Status != TargetStatusExpired {
			t.Fatalf("listed expired target status = %q, want %q", listed.Targets[0].Status, TargetStatusExpired)
		}
		return
	}
	t.Fatalf("expired job %q not found in List()", expiredJob.ID)
}

// TestAcknowledgedJobsExpireAfterTTL verifies P2-LOG-05 step 2: the
// PruneAcknowledgedTargets worker transitions long-acknowledged-no-result
// targets to expired so the CP stops re-dispatching commands the agent has
// already forgotten (its own idempotency cache has a matching 2h window).
func TestAcknowledgedJobsExpireAfterTTL(t *testing.T) {
	const ackTTL = 2 * time.Hour
	const retryAfter = 30 * time.Second

	start := time.Date(2026, time.April, 2, 13, 0, 0, 0, time.UTC)
	now := start
	service := NewService()
	service.SetNow(func() time.Time { return now })

	// Large TTL so the job itself does not expire via jobShouldExpire
	// before the ack-expiry worker gets a chance to fire.
	job, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            24 * time.Hour,
		IdempotencyKey: "ack-expire",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	service.MarkDelivered(context.Background(), "agent-1", job.ID, now.Add(time.Second))
	service.MarkAcknowledged(context.Background(), "agent-1", job.ID, now.Add(2*time.Second))

	// Before TTL elapses: PruneAcknowledgedTargets is a no-op.
	if expired := service.PruneAcknowledgedTargets(context.Background(), ackTTL); expired != 0 {
		t.Fatalf("PruneAcknowledgedTargets pre-TTL = %d, want 0", expired)
	}

	// Advance past the 2h ack TTL.
	now = start.Add(ackTTL + time.Minute)

	expired := service.PruneAcknowledgedTargets(context.Background(), ackTTL)
	if expired != 1 {
		t.Fatalf("PruneAcknowledgedTargets post-TTL = %d, want 1", expired)
	}

	list := service.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d, want 1", len(list))
	}
	if list[0].Targets[0].Status != TargetStatusExpired {
		t.Fatalf("Targets[0].Status = %q, want %q", list[0].Targets[0].Status, TargetStatusExpired)
	}
	if list[0].Status != StatusExpired {
		t.Fatalf("jobs[0].Status = %q, want %q", list[0].Status, StatusExpired)
	}

	// Re-dispatch must no longer fire — the target is expired and the
	// agent-side idempotency cache can no longer deduplicate.
	if pending := service.PendingForAgent(context.Background(), "agent-1", retryAfter); len(pending) != 0 {
		t.Fatalf("len(PendingForAgent) = %d after ack expiry, want 0", len(pending))
	}

	// Idempotency: a late JobResult arriving after expiry (and after
	// terminal-key eviction wipes the job entirely) must be treated as a
	// non-fatal warn, not a crash. Advance the clock past a short eviction
	// TTL so PruneKeys drops the terminal-state record.
	now = now.Add(time.Hour)
	if evicted := service.PruneKeys(time.Minute); evicted != 1 {
		t.Fatalf("PruneKeys post-expiry = %d, want 1", evicted)
	}
	if service.RecordResult(context.Background(), "agent-1", job.ID, true, "late ok", "", now.Add(time.Second)) {
		t.Fatal("RecordResult on evicted job = true, want false (idempotent safety net)")
	}
}

// TestStartAcknowledgedExpiryWorker exercises the ticker wiring.
func TestStartAcknowledgedExpiryWorker(t *testing.T) {
	const ackTTL = 50 * time.Millisecond

	start := time.Date(2026, time.April, 2, 14, 0, 0, 0, time.UTC)
	var (
		clockMu sync.Mutex
		now     = start
	)
	service := NewService()
	service.SetNow(func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	})

	job, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "ack-worker",
		ActorID:        "user-1",
	}, start)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	service.MarkDelivered(context.Background(), "agent-1", job.ID, start.Add(time.Millisecond))
	service.MarkAcknowledged(context.Background(), "agent-1", job.ID, start.Add(2*time.Millisecond))

	// Advance past the TTL so the ticker's first scan marks the target
	// expired on its next fire.
	clockMu.Lock()
	now = start.Add(ackTTL + 10*time.Millisecond)
	clockMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	service.StartAcknowledgedExpiryWorker(ctx, 10*time.Millisecond, ackTTL, &wg)

	deadline := time.Now().Add(2 * time.Second)
	for {
		list := service.List()
		if len(list) == 1 && list[0].Targets[0].Status == TargetStatusExpired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("StartAcknowledgedExpiryWorker did not expire acked target within 2s")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	wg.Wait()
}

// TestRecordResultReportsUnknownJob verifies the idempotent safety net:
// RecordResult for a job the service has never seen (or has already
// evicted) returns false so the caller can log a warn instead of silently
// dropping the result.
func TestRecordResultReportsUnknownJob(t *testing.T) {
	service := NewService()
	if service.RecordResult(context.Background(), "agent-1", "job-never-existed", true, "ok", "", time.Now()) {
		t.Fatal("RecordResult on unknown job = true, want false")
	}
}

// TestServiceGetReturnsJobByID verifies P-4: Get is an O(1) lookup that
// supersedes the historical pattern of scanning ListWithContext for a
// single job ID.
func TestServiceGetReturnsJobByID(t *testing.T) {
	service := NewService()
	now := time.Now().UTC()

	job, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "key-get-1",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	got, ok := service.Get(job.ID)
	if !ok {
		t.Fatalf("Get(%q) ok = false, want true", job.ID)
	}
	if got.ID != job.ID {
		t.Fatalf("Get(%q).ID = %q, want %q", job.ID, got.ID, job.ID)
	}

	// Unknown ID must return (zero, false).
	if _, ok := service.Get("job-does-not-exist"); ok {
		t.Fatal("Get(unknown) ok = true, want false")
	}
}

// TestServiceLatestSucceededWithContextEmpty verifies P-4: a service with no
// recorded results must report (nil, false) for any clientID.
func TestServiceLatestSucceededWithContextEmpty(t *testing.T) {
	service := NewService()
	if got, ok := service.LatestSucceededWithContext(context.Background(), "client-1"); ok || got != nil {
		t.Fatalf("LatestSucceededWithContext on empty service = (%v, %v), want (nil, false)", got, ok)
	}
	if _, ok := service.LatestSucceededWithContext(context.Background(), ""); ok {
		t.Fatal("LatestSucceededWithContext(\"\") ok = true, want false (empty clientID rejected)")
	}
}

// TestServiceLatestSucceededWithContextReturnsLatestSucceeded verifies P-4
// end-to-end: with two succeeded client.create jobs for the same clientID
// plus one failed job, the API returns the latest succeeded job by
// CreatedAt and ignores the failed one. This is the spec's headline
// scenario for the new index.
func TestServiceLatestSucceededWithContextReturnsLatestSucceeded(t *testing.T) {
	service := NewService()
	clientID := "client-42"
	payload := func() string {
		return `{"client_id":"` + clientID + `","name":"alice"}`
	}

	// Pin the service clock so jobs do not wander into Expired between
	// Enqueue and RecordResult — the in-memory expiry path uses s.now()
	// against TTL on every ListWithContext / updateTarget call.
	now1 := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	currentNow := now1
	service.SetNow(func() time.Time { return currentNow })
	job1, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionClientCreate,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "key-cs1",
		ActorID:        "user-1",
		PayloadJSON:    payload(),
	}, now1)
	if err != nil {
		t.Fatalf("Enqueue(job1) error = %v", err)
	}
	service.MarkDelivered(context.Background(), "agent-1", job1.ID, now1)
	service.MarkAcknowledged(context.Background(), "agent-1", job1.ID, now1)
	if !service.RecordResult(context.Background(), "agent-1", job1.ID, true, "ok", `{"connection_links":["link1"]}`, now1) {
		t.Fatal("RecordResult(job1) = false, want true")
	}

	// Job 2: client.update for clientID, succeeds at a later time.
	now2 := now1.Add(10 * time.Minute)
	currentNow = now2
	job2, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionClientUpdate,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "key-cs2",
		ActorID:        "user-1",
		PayloadJSON:    payload(),
	}, now2)
	if err != nil {
		t.Fatalf("Enqueue(job2) error = %v", err)
	}
	service.MarkDelivered(context.Background(), "agent-1", job2.ID, now2)
	service.MarkAcknowledged(context.Background(), "agent-1", job2.ID, now2)
	if !service.RecordResult(context.Background(), "agent-1", job2.ID, true, "ok", `{"connection_links":["link2"]}`, now2) {
		t.Fatal("RecordResult(job2) = false, want true")
	}

	// Job 3: client.update for clientID, fails. Must NOT update the index.
	now3 := now2.Add(10 * time.Minute)
	currentNow = now3
	job3, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionClientUpdate,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "key-cs3",
		ActorID:        "user-1",
		PayloadJSON:    payload(),
	}, now3)
	if err != nil {
		t.Fatalf("Enqueue(job3) error = %v", err)
	}
	service.MarkDelivered(context.Background(), "agent-1", job3.ID, now3)
	service.MarkAcknowledged(context.Background(), "agent-1", job3.ID, now3)
	if !service.RecordResult(context.Background(), "agent-1", job3.ID, false, "boom", "", now3) {
		t.Fatal("RecordResult(job3 fail) = false, want true")
	}

	got, ok := service.LatestSucceededWithContext(context.Background(), clientID)
	if !ok || got == nil {
		t.Fatal("LatestSucceededWithContext after 2-success-1-fail = (nil,false), want hit")
	}
	if got.ID != job2.ID {
		t.Fatalf("LatestSucceededWithContext.ID = %q, want %q (latest succeeded job)", got.ID, job2.ID)
	}
	if got.Status != StatusSucceeded {
		t.Fatalf("LatestSucceededWithContext.Status = %q, want %q", got.Status, StatusSucceeded)
	}

	// A clientID we have not seen still returns miss.
	if _, ok := service.LatestSucceededWithContext(context.Background(), "client-other"); ok {
		t.Fatal("LatestSucceededWithContext(unknown clientID) ok = true, want false")
	}
}

func TestConfigApplyActionValid(t *testing.T) {
	if !IsValidAction(ActionConfigApply) {
		t.Fatalf("config.apply must be a valid action")
	}
}

func TestConfigFetchActionValid(t *testing.T) {
	if !IsValidAction(ActionConfigFetch) {
		t.Fatalf("config.fetch must be a valid action")
	}
}

// TestEnqueueGeneratesIdempotencyKeyWhenEmpty guards A4: callers that omit
// the idempotency key (config.apply rolling fan-out, self-update dispatch)
// must each get a unique generated key instead of all colliding on "" —
// previously the second empty-key Enqueue failed with
// ErrDuplicateIdempotencyKey and the "" slot was never evicted.
func TestEnqueueGeneratesIdempotencyKeyWhenEmpty(t *testing.T) {
	now := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	first, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionConfigApply,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            5 * time.Minute,
		IdempotencyKey: "",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue(first) error = %v", err)
	}
	second, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionConfigApply,
		TargetAgentIDs: []string{"agent-2"},
		TTL:            5 * time.Minute,
		IdempotencyKey: "",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue(second) error = %v, want nil (empty keys must not collide)", err)
	}
	if first.IdempotencyKey == "" || second.IdempotencyKey == "" {
		t.Fatalf("generated keys must be non-empty, got %q and %q", first.IdempotencyKey, second.IdempotencyKey)
	}
	if first.IdempotencyKey == second.IdempotencyKey {
		t.Fatalf("generated keys must be unique, both = %q", first.IdempotencyKey)
	}
	if _, ok := service.keys[""]; ok {
		t.Fatal("the empty-string key slot must never be reserved")
	}
}

// TestUpdateTargetUsesPanelClockNotAgentClock guards D3: redelivery gating
// in targetIsPending compares target.UpdatedAt with the panel's s.now(),
// so UpdatedAt must be stamped with the panel clock. Stamping the
// agent-supplied ObservedAt froze redelivery for agents whose clock runs
// ahead (UpdatedAt in the future => retry window never elapses).
func TestUpdateTargetUsesPanelClockNotAgentClock(t *testing.T) {
	panelNow := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return panelNow })

	job, err := service.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "panel-clock",
		ActorID:        "user-1",
	}, panelNow)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	// Agent clock is 2h ahead. With agent-clock stamping the target looks
	// "fresh" for two extra hours and scheduled redelivery never fires.
	agentObservedAt := panelNow.Add(2 * time.Hour)
	service.MarkDelivered(context.Background(), "agent-1", job.ID, agentObservedAt)

	got, ok := service.Get(job.ID)
	if !ok {
		t.Fatalf("job %q not found", job.ID)
	}
	if !got.Targets[0].UpdatedAt.Equal(panelNow) {
		t.Fatalf("target.UpdatedAt = %v, want panel clock %v", got.Targets[0].UpdatedAt, panelNow)
	}
}
