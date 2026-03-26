package jobs

import (
	"context"
	"errors"
	"path/filepath"
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
	if jobs[0].Status != StatusFailed {
		t.Fatalf("jobs[0].Status = %q, want %q", jobs[0].Status, StatusFailed)
	}

	stored := service.jobs[job.ID]
	if stored.Status != StatusQueued {
		t.Fatalf("stored.Status = %q, want %q", stored.Status, StatusQueued)
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
