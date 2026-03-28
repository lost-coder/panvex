package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestServiceEnqueueRejectsDuplicateIdempotencyKey(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	first, err := service.Enqueue(CreateJobInput{
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

	_, err = service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "same-key",
		ActorID:        "user-1",
	}, now.Add(10*time.Second))
	if err == nil {
		t.Fatal("Enqueue() duplicate error = nil, want idempotency failure")
	}

	if err != ErrDuplicateIdempotencyKey {
		t.Fatalf("Enqueue() duplicate error = %v, want %v", err, ErrDuplicateIdempotencyKey)
	}
}

func TestServiceEnqueueRejectsMutatingActionForReadOnlyTarget(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	_, err := service.Enqueue(CreateJobInput{
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

	if err != ErrReadOnlyTarget {
		t.Fatalf("Enqueue() error = %v, want %v", err, ErrReadOnlyTarget)
	}
}

func TestServiceEnqueueRejectsDuplicateIdempotencyKeyAfterRestart(t *testing.T) {
	now := time.Date(2026, time.March, 15, 11, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := NewServiceWithStore(store)
	job, err := first.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "same-key",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	restored := NewServiceWithStore(store)
	if _, err := restored.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "same-key",
		ActorID:        "user-1",
	}, now.Add(time.Minute)); err != ErrDuplicateIdempotencyKey {
		t.Fatalf("Enqueue() duplicate after restart error = %v, want %v", err, ErrDuplicateIdempotencyKey)
	}

	if len(restored.List()) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(restored.List()), 1)
	}

	if restored.List()[0].ID != job.ID {
		t.Fatalf("restored.List()[0].ID = %q, want %q", restored.List()[0].ID, job.ID)
	}
}

func TestServiceRecordResultPersistsTargetsAcrossRestart(t *testing.T) {
	now := time.Date(2026, time.March, 15, 11, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := NewServiceWithStore(store)
	first.SetNow(func() time.Time {
		return now
	})
	job, err := first.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1", "agent-2"},
		TTL:            time.Minute,
		IdempotencyKey: "reload-two",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	first.MarkDelivered("agent-1", job.ID, now.Add(5*time.Second))
	first.MarkDelivered("agent-2", job.ID, now.Add(5*time.Second))
	first.RecordResult("agent-1", job.ID, true, "ok", "", now.Add(10*time.Second))
	first.RecordResult("agent-2", job.ID, false, "reload failed", "", now.Add(11*time.Second))

	restored := NewServiceWithStore(store)
	restored.SetNow(func() time.Time {
		return now.Add(20 * time.Second)
	})
	jobs := restored.List()
	if len(jobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(jobs), 1)
	}

	if jobs[0].Status != StatusFailed {
		t.Fatalf("jobs[0].Status = %q, want %q", jobs[0].Status, StatusFailed)
	}

	if len(jobs[0].Targets) != 2 {
		t.Fatalf("len(jobs[0].Targets) = %d, want %d", len(jobs[0].Targets), 2)
	}

	if jobs[0].Targets[0].Status == jobs[0].Targets[1].Status {
		t.Fatalf("target statuses = %q and %q, want one success and one failure", jobs[0].Targets[0].Status, jobs[0].Targets[1].Status)
	}
}

func TestServicePersistsStructuredClientPayloadAndResultAcrossRestart(t *testing.T) {
	now := time.Date(2026, time.March, 17, 16, 45, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := NewServiceWithStore(store)
	first.SetNow(func() time.Time {
		return now
	})
	job, err := first.Enqueue(CreateJobInput{
		Action:         ActionClientCreate,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "client-create",
		ActorID:        "user-1",
		PayloadJSON:    `{"client_id":"client-1","secret":"secret-1"}`,
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	first.MarkDelivered("agent-1", job.ID, now.Add(5*time.Second))
	first.RecordResult("agent-1", job.ID, true, "applied", `{"connection_link":"tg://proxy?server=node-a&secret=secret-1"}`, now.Add(10*time.Second))

	restored := NewServiceWithStore(store)
	restored.SetNow(func() time.Time {
		return now.Add(20 * time.Second)
	})
	jobs := restored.List()
	if len(jobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(jobs), 1)
	}
	if jobs[0].PayloadJSON != `{"client_id":"client-1","secret":"secret-1"}` {
		t.Fatalf("jobs[0].PayloadJSON = %q, want %q", jobs[0].PayloadJSON, `{"client_id":"client-1","secret":"secret-1"}`)
	}
	if len(jobs[0].Targets) != 1 {
		t.Fatalf("len(jobs[0].Targets) = %d, want %d", len(jobs[0].Targets), 1)
	}
	if jobs[0].Targets[0].ResultJSON != `{"connection_link":"tg://proxy?server=node-a&secret=secret-1"}` {
		t.Fatalf("jobs[0].Targets[0].ResultJSON = %q, want %q", jobs[0].Targets[0].ResultJSON, `{"connection_link":"tg://proxy?server=node-a&secret=secret-1"}`)
	}
}

func TestServiceMarkDeliveredKeepsInMemoryStateWhenPersistenceFails(t *testing.T) {
	now := time.Now().UTC()
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &failingJobStore{JobStore: sqliteStore}
	service := NewServiceWithStore(store)
	job, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "deliver-with-store-error",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	store.putJobErr = errors.New("put job failed")

	service.MarkDelivered("agent-1", job.ID, now.Add(5*time.Second))

	jobs := service.List()
	if len(jobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(jobs), 1)
	}
	if jobs[0].Status != StatusRunning {
		t.Fatalf("jobs[0].Status = %q, want %q", jobs[0].Status, StatusRunning)
	}
	if jobs[0].Targets[0].Status != TargetStatusDelivered {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", jobs[0].Targets[0].Status, TargetStatusDelivered)
	}
}

func TestServiceMarkAcknowledgedTransitionsTargetState(t *testing.T) {
	now := time.Date(2026, time.March, 18, 10, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time {
		return now.Add(10 * time.Second)
	})

	job, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "ack-transition",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	service.MarkDelivered("agent-1", job.ID, now.Add(2*time.Second))
	service.MarkAcknowledged("agent-1", job.ID, now.Add(3*time.Second))

	jobs := service.List()
	if len(jobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(jobs), 1)
	}
	if jobs[0].Status != StatusRunning {
		t.Fatalf("jobs[0].Status = %q, want %q", jobs[0].Status, StatusRunning)
	}
	if jobs[0].Targets[0].Status != TargetStatusAcknowledged {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", jobs[0].Targets[0].Status, TargetStatusAcknowledged)
	}
}

func TestServiceMarkDeliveredDoesNotDowngradeAcknowledgedTarget(t *testing.T) {
	now := time.Date(2026, time.March, 18, 10, 30, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time {
		return now.Add(10 * time.Second)
	})

	job, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "ack-no-downgrade",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	service.MarkDelivered("agent-1", job.ID, now.Add(2*time.Second))
	service.MarkAcknowledged("agent-1", job.ID, now.Add(3*time.Second))
	service.MarkDelivered("agent-1", job.ID, now.Add(4*time.Second))

	jobs := service.List()
	if len(jobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(jobs), 1)
	}
	if jobs[0].Status != StatusRunning {
		t.Fatalf("jobs[0].Status = %q, want %q", jobs[0].Status, StatusRunning)
	}
	if jobs[0].Targets[0].Status != TargetStatusAcknowledged {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", jobs[0].Targets[0].Status, TargetStatusAcknowledged)
	}
}

func TestServiceMarkAcknowledgedIgnoresQueuedTarget(t *testing.T) {
	now := time.Date(2026, time.March, 18, 10, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time {
		return now.Add(10 * time.Second)
	})

	job, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "ack-queued",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	service.MarkAcknowledged("agent-1", job.ID, now.Add(5*time.Second))

	jobs := service.List()
	if len(jobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(jobs), 1)
	}
	if jobs[0].Status != StatusQueued {
		t.Fatalf("jobs[0].Status = %q, want %q", jobs[0].Status, StatusQueued)
	}
	if jobs[0].Targets[0].Status != TargetStatusQueued {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", jobs[0].Targets[0].Status, TargetStatusQueued)
	}
}

func TestServicePendingForAgentReturnsQueuedAndStaleSentJobs(t *testing.T) {
	const retryAfter = 30 * time.Second
	now := time.Date(2026, time.March, 19, 9, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time {
		return now
	})

	queued, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-queued",
		ActorID:        "user-1",
	}, now.Add(-3*time.Minute))
	if err != nil {
		t.Fatalf("Enqueue(queued) error = %v", err)
	}
	staleSent, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-stale-sent",
		ActorID:        "user-1",
	}, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("Enqueue(staleSent) error = %v", err)
	}
	recentSent, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-recent-sent",
		ActorID:        "user-1",
	}, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("Enqueue(recentSent) error = %v", err)
	}
	otherAgent, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-2"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-other-agent",
		ActorID:        "user-1",
	}, now.Add(-30*time.Second))
	if err != nil {
		t.Fatalf("Enqueue(otherAgent) error = %v", err)
	}

	service.MarkDelivered("agent-1", staleSent.ID, now.Add(-(retryAfter + time.Second)))
	service.MarkDelivered("agent-1", recentSent.ID, now.Add(-(retryAfter - time.Second)))

	pending := service.PendingForAgent("agent-1", retryAfter)
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

func TestServicePendingForAgentWorksAfterRestore(t *testing.T) {
	const retryAfter = 30 * time.Second
	now := time.Date(2026, time.March, 19, 9, 30, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := NewServiceWithStore(store)
	job, err := first.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-after-restore",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	restored := NewServiceWithStore(store)
	restored.SetNow(func() time.Time {
		return now.Add(time.Minute)
	})
	pending := restored.PendingForAgent("agent-1", retryAfter)
	if len(pending) != 1 {
		t.Fatalf("len(PendingForAgent) = %d, want %d", len(pending), 1)
	}
	if pending[0].ID != job.ID {
		t.Fatalf("pending[0].ID = %q, want %q", pending[0].ID, job.ID)
	}
}

func TestServicePendingForAgentDropsAcknowledgedJobFromIndex(t *testing.T) {
	const retryAfter = 30 * time.Second
	now := time.Date(2026, time.March, 19, 9, 45, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time {
		return now
	})

	job, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-index-prune-ack",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	service.MarkDelivered("agent-1", job.ID, now.Add(time.Second))
	service.MarkAcknowledged("agent-1", job.ID, now.Add(2*time.Second))

	pending := service.PendingForAgent("agent-1", retryAfter)
	if len(pending) != 0 {
		t.Fatalf("len(PendingForAgent) = %d, want %d", len(pending), 0)
	}
	if agentJobs := service.agentJobs["agent-1"]; agentJobs != nil {
		if _, exists := agentJobs[job.ID]; exists {
			t.Fatalf("agentJobs[agent-1] still contains %q after acknowledgement", job.ID)
		}
	}
}

func TestServiceListProjectsExpiredQueuedJobsAsFailed(t *testing.T) {
	service := NewService()
	now := time.Now().UTC().Add(-2 * time.Minute)

	job, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "expired-job",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	jobs := service.List()
	if len(jobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(jobs), 1)
	}
	if jobs[0].ID != job.ID {
		t.Fatalf("jobs[0].ID = %q, want %q", jobs[0].ID, job.ID)
	}
	if jobs[0].Status != StatusExpired {
		t.Fatalf("jobs[0].Status = %q, want %q", jobs[0].Status, StatusExpired)
	}
	if jobs[0].Targets[0].Status != TargetStatusExpired {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", jobs[0].Targets[0].Status, TargetStatusExpired)
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

	job, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "expired-result",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	service.RecordResult("agent-1", job.ID, true, "late success", "", now.Add(3*time.Minute))

	jobs := service.List()
	if len(jobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(jobs), 1)
	}
	if jobs[0].Status != StatusExpired {
		t.Fatalf("jobs[0].Status = %q, want %q", jobs[0].Status, StatusExpired)
	}
	if jobs[0].Targets[0].Status != TargetStatusExpired {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", jobs[0].Targets[0].Status, TargetStatusExpired)
	}
	if jobs[0].Targets[0].ResultText != "" {
		t.Fatalf("jobs[0].Targets[0].ResultText = %q, want empty string", jobs[0].Targets[0].ResultText)
	}
}

func TestServiceUpdateTargetDoesNotExpireUnrelatedJobs(t *testing.T) {
	baseNow := time.Date(2026, time.March, 26, 11, 0, 0, 0, time.UTC)
	currentTime := baseNow
	service := NewService()
	service.SetNow(func() time.Time {
		return currentTime
	})

	expiredJob, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-expired"},
		TTL:            time.Minute,
		IdempotencyKey: "unrelated-expired",
		ActorID:        "user-1",
	}, baseNow)
	if err != nil {
		t.Fatalf("Enqueue(expired) error = %v", err)
	}

	liveJob, err := service.Enqueue(CreateJobInput{
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
	service.MarkDelivered("agent-live", liveJob.ID, currentTime)

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

func TestServiceListPersistsExpiredQueuedJobsAcrossRestart(t *testing.T) {
	now := time.Date(2026, time.March, 19, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := NewServiceWithStore(store)
	job, err := first.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "persist-expired",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	first.SetNow(func() time.Time {
		return now.Add(2 * time.Minute)
	})
	jobs := first.List()
	if len(jobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(jobs), 1)
	}
	if jobs[0].Status != StatusExpired {
		t.Fatalf("jobs[0].Status = %q, want %q", jobs[0].Status, StatusExpired)
	}
	if jobs[0].Targets[0].Status != TargetStatusExpired {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", jobs[0].Targets[0].Status, TargetStatusExpired)
	}

	restored := NewServiceWithStore(store)
	restoredJobs := restored.List()
	if len(restoredJobs) != 1 {
		t.Fatalf("len(restored.List()) = %d, want %d", len(restoredJobs), 1)
	}
	if restoredJobs[0].ID != job.ID {
		t.Fatalf("restored.List()[0].ID = %q, want %q", restoredJobs[0].ID, job.ID)
	}
	if restoredJobs[0].Status != StatusExpired {
		t.Fatalf("restored.List()[0].Status = %q, want %q", restoredJobs[0].Status, StatusExpired)
	}
	if restoredJobs[0].Targets[0].Status != TargetStatusExpired {
		t.Fatalf("restored.List()[0].Targets[0].Status = %q, want %q", restoredJobs[0].Targets[0].Status, TargetStatusExpired)
	}
}

func TestServiceListAllowsConcurrentUpdateWhileExpirationPersistenceBlocks(t *testing.T) {
	baseNow := time.Date(2026, time.March, 20, 13, 0, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &blockingJobStore{JobStore: sqliteStore}
	service := NewServiceWithStore(store)
	expiredJob, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-expired"},
		TTL:            time.Minute,
		IdempotencyKey: "expired-for-list-blocking",
		ActorID:        "user-1",
	}, baseNow.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("Enqueue(expired) error = %v", err)
	}
	liveJob, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-live"},
		TTL:            time.Hour,
		IdempotencyKey: "live-for-list-blocking",
		ActorID:        "user-1",
	}, baseNow)
	if err != nil {
		t.Fatalf("Enqueue(live) error = %v", err)
	}
	service.SetNow(func() time.Time {
		return baseNow
	})

	putJobStarted := make(chan struct{})
	releasePutJob := make(chan struct{})
	store.blockNextPutJob(putJobStarted, releasePutJob)

	listDone := make(chan []Job, 1)
	go func() {
		listDone <- service.List()
	}()

	select {
	case <-putJobStarted:
	case <-time.After(time.Second):
		t.Fatal("List() persistence did not block, want blocked PutJob")
	}

	markDone := make(chan struct{})
	go func() {
		service.MarkDelivered("agent-live", liveJob.ID, baseNow.Add(10*time.Second))
		close(markDone)
	}()

	select {
	case <-markDone:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("MarkDelivered() blocked while List() persistence was stalled")
	}

	close(releasePutJob)

	select {
	case listedJobs := <-listDone:
		if len(listedJobs) != 2 {
			t.Fatalf("len(List()) = %d, want %d", len(listedJobs), 2)
		}
	case <-time.After(time.Second):
		t.Fatal("List() did not complete after persistence release")
	}

	jobs := service.List()
	if len(jobs) != 2 {
		t.Fatalf("len(List()) after unblock = %d, want %d", len(jobs), 2)
	}
	var foundExpired bool
	for _, job := range jobs {
		if job.ID != expiredJob.ID {
			continue
		}
		foundExpired = true
		if job.Status != StatusExpired {
			t.Fatalf("expired job status = %q, want %q", job.Status, StatusExpired)
		}
	}
	if !foundExpired {
		t.Fatalf("expired job %q not found in list", expiredJob.ID)
	}
}

func TestServiceMarkDeliveredAllowsConcurrentListWhilePersistenceBlocks(t *testing.T) {
	now := time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &blockingJobStore{JobStore: sqliteStore}
	service := NewServiceWithStore(store)
	service.SetNow(func() time.Time {
		return now.Add(10 * time.Second)
	})
	job, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "mark-delivered-list-unblocked",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	putJobStarted := make(chan struct{})
	releasePutJob := make(chan struct{})
	store.blockNextPutJob(putJobStarted, releasePutJob)

	markDone := make(chan struct{})
	go func() {
		service.MarkDelivered("agent-1", job.ID, now.Add(5*time.Second))
		close(markDone)
	}()

	select {
	case <-putJobStarted:
	case <-time.After(time.Second):
		t.Fatal("PutJob() did not block, want blocked persistence")
	}

	listDone := make(chan []Job, 1)
	go func() {
		listDone <- service.List()
	}()

	select {
	case listedJobs := <-listDone:
		if len(listedJobs) != 1 {
			t.Fatalf("len(List()) = %d, want %d", len(listedJobs), 1)
		}
		if listedJobs[0].Status != StatusRunning {
			t.Fatalf("jobs[0].Status = %q, want %q", listedJobs[0].Status, StatusRunning)
		}
		if listedJobs[0].Targets[0].Status != TargetStatusSent {
			t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", listedJobs[0].Targets[0].Status, TargetStatusSent)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("List() blocked while persistence was stalled")
	}

	close(releasePutJob)

	select {
	case <-markDone:
	case <-time.After(time.Second):
		t.Fatal("MarkDelivered() did not complete after persistence release")
	}
}

func TestServiceUpdateTargetPersistsLatestVersionAfterOutOfOrderWrites(t *testing.T) {
	now := time.Date(2026, time.March, 20, 12, 30, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &blockingJobStore{JobStore: sqliteStore}
	service := NewServiceWithStore(store)
	service.SetNow(func() time.Time {
		return now.Add(10 * time.Second)
	})
	job, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "persist-latest-version",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	putJobStarted := make(chan struct{})
	releasePutJob := make(chan struct{})
	store.blockNextPutJob(putJobStarted, releasePutJob)

	markDone := make(chan struct{})
	go func() {
		service.MarkDelivered("agent-1", job.ID, now.Add(5*time.Second))
		close(markDone)
	}()

	select {
	case <-putJobStarted:
	case <-time.After(time.Second):
		t.Fatal("PutJob() did not block, want out-of-order write setup")
	}

	service.RecordResult("agent-1", job.ID, false, "failed", "", now.Add(6*time.Second))

	close(releasePutJob)

	select {
	case <-markDone:
	case <-time.After(time.Second):
		t.Fatal("MarkDelivered() did not complete after persistence release")
	}

	restored := NewServiceWithStore(sqliteStore)
	restoredJobs := restored.List()
	if len(restoredJobs) != 1 {
		t.Fatalf("len(restored.List()) = %d, want %d", len(restoredJobs), 1)
	}
	if restoredJobs[0].Status != StatusFailed {
		t.Fatalf("restored.List()[0].Status = %q, want %q", restoredJobs[0].Status, StatusFailed)
	}
	if len(restoredJobs[0].Targets) != 1 {
		t.Fatalf("len(restored.List()[0].Targets) = %d, want %d", len(restoredJobs[0].Targets), 1)
	}
	if restoredJobs[0].Targets[0].Status != TargetStatusFailed {
		t.Fatalf("restored.List()[0].Targets[0].Status = %q, want %q", restoredJobs[0].Targets[0].Status, TargetStatusFailed)
	}
	if restoredJobs[0].Targets[0].ResultText != "failed" {
		t.Fatalf("restored.List()[0].Targets[0].ResultText = %q, want %q", restoredJobs[0].Targets[0].ResultText, "failed")
	}
}

func TestNewServiceWithStoreRecordsRestoreError(t *testing.T) {
	store := &failingJobStore{
		listJobsErr: errors.New("list jobs failed"),
	}

	service := NewServiceWithStore(store)

	if service.StartupError() == nil {
		t.Fatal("StartupError() = nil, want restore failure")
	}
}

type failingJobStore struct {
	storage.JobStore
	putJobErr  error
	listJobsErr error
}

func (s *failingJobStore) PutJob(ctx context.Context, job storage.JobRecord) error {
	if s.putJobErr != nil {
		return s.putJobErr
	}

	return s.JobStore.PutJob(ctx, job)
}

func (s *failingJobStore) ListJobs(ctx context.Context) ([]storage.JobRecord, error) {
	if s.listJobsErr != nil {
		return nil, s.listJobsErr
	}
	if s.JobStore == nil {
		return nil, nil
	}

	return s.JobStore.ListJobs(ctx)
}

type blockingJobStore struct {
	storage.JobStore
	mu             sync.Mutex
	putJobStarted  chan<- struct{}
	putJobRelease  <-chan struct{}
	blockNextPut   bool
}

func (s *blockingJobStore) blockNextPutJob(started chan<- struct{}, release <-chan struct{}) {
	s.mu.Lock()
	s.putJobStarted = started
	s.putJobRelease = release
	s.blockNextPut = true
	s.mu.Unlock()
}

func (s *blockingJobStore) PutJob(ctx context.Context, job storage.JobRecord) error {
	s.mu.Lock()
	block := s.blockNextPut
	started := s.putJobStarted
	release := s.putJobRelease
	if block {
		s.blockNextPut = false
		s.putJobStarted = nil
		s.putJobRelease = nil
	}
	s.mu.Unlock()

	if block {
		if started != nil {
			close(started)
		}
		if release != nil {
			<-release
		}
	}

	return s.JobStore.PutJob(ctx, job)
}
