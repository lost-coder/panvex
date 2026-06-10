// service_integration_test.go uses package jobs_test (external) so that
// storage/sqlite/jobs_repository.go can import package jobs without creating
// an import cycle. Tests that require white-box access to unexported fields
// (service.keys, service.mu, service.jobs, service.agentJobs) remain in
// service_test.go under package jobs.
package jobs_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// ---------------------------------------------------------------------------
// Test store helpers
// ---------------------------------------------------------------------------

type failingJobStore struct {
	storage.JobStore
	putJobErr   error
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
	mu            sync.Mutex
	putJobStarted chan<- struct{}
	putJobRelease <-chan struct{}
	blockNextPut  bool
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

// ---------------------------------------------------------------------------
// Persistence / restart tests
// ---------------------------------------------------------------------------

func TestServiceEnqueueRejectsDuplicateIdempotencyKeyAfterRestart(t *testing.T) {
	now := time.Date(2026, time.March, 15, 11, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := jobs.NewServiceWithStore(store)
	job, err := first.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "same-key",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	restored := jobs.NewServiceWithStore(store)
	if _, err := restored.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "same-key",
		ActorID:        "user-1",
	}, now.Add(time.Minute)); !errors.Is(err, jobs.ErrDuplicateIdempotencyKey) {
		t.Fatalf("Enqueue() duplicate after restart error = %v, want %v", err, jobs.ErrDuplicateIdempotencyKey)
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

	first := jobs.NewServiceWithStore(store)
	first.SetNow(func() time.Time { return now })
	job, err := first.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1", "agent-2"},
		TTL:            time.Minute,
		IdempotencyKey: "reload-two",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	first.MarkDelivered(context.Background(), "agent-1", job.ID, now.Add(5*time.Second))
	first.MarkDelivered(context.Background(), "agent-2", job.ID, now.Add(5*time.Second))
	first.RecordResult(context.Background(), "agent-1", job.ID, true, "ok", "", now.Add(10*time.Second))
	first.RecordResult(context.Background(), "agent-2", job.ID, false, "reload failed", "", now.Add(11*time.Second))

	restored := jobs.NewServiceWithStore(store)
	restored.SetNow(func() time.Time { return now.Add(20 * time.Second) })
	list := restored.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(list), 1)
	}
	// Mixed terminal outcome (F2): agent-1 succeeded, agent-2 failed. This is
	// surfaced as "partial" rather than collapsing to "failed" — the sibling
	// success must not be masked by the failure.
	if list[0].Status != jobs.StatusPartial {
		t.Fatalf("jobs[0].Status = %q, want %q", list[0].Status, jobs.StatusPartial)
	}
	if len(list[0].Targets) != 2 {
		t.Fatalf("len(jobs[0].Targets) = %d, want %d", len(list[0].Targets), 2)
	}
	if list[0].Targets[0].Status == list[0].Targets[1].Status {
		t.Fatalf("target statuses = %q and %q, want one success and one failure", list[0].Targets[0].Status, list[0].Targets[1].Status)
	}
}

func TestServicePersistsStructuredClientPayloadAndResultAcrossRestart(t *testing.T) {
	now := time.Date(2026, time.March, 17, 16, 45, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := jobs.NewServiceWithStore(store)
	first.SetNow(func() time.Time { return now })
	job, err := first.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionClientCreate,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "client-create",
		ActorID:        "user-1",
		PayloadJSON:    `{"client_id":"client-1","secret":"secret-1"}`,
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	first.MarkDelivered(context.Background(), "agent-1", job.ID, now.Add(5*time.Second))
	first.RecordResult(context.Background(), "agent-1", job.ID, true, "applied", `{"connection_links":["tg://proxy?server=node-a&secret=secret-1"]}`, now.Add(10*time.Second))

	restored := jobs.NewServiceWithStore(store)
	restored.SetNow(func() time.Time { return now.Add(20 * time.Second) })
	list := restored.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(list), 1)
	}
	if list[0].PayloadJSON != `{"client_id":"client-1","secret":"secret-1"}` {
		t.Fatalf("jobs[0].PayloadJSON = %q, want %q", list[0].PayloadJSON, `{"client_id":"client-1","secret":"secret-1"}`)
	}
	if len(list[0].Targets) != 1 {
		t.Fatalf("len(jobs[0].Targets) = %d, want %d", len(list[0].Targets), 1)
	}
	if list[0].Targets[0].ResultJSON != `{"connection_links":["tg://proxy?server=node-a&secret=secret-1"]}` {
		t.Fatalf("jobs[0].Targets[0].ResultJSON = %q, want %q", list[0].Targets[0].ResultJSON, `{"connection_links":["tg://proxy?server=node-a&secret=secret-1"]}`)
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
	service := jobs.NewServiceWithStore(store)
	job, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "deliver-with-store-error",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	store.putJobErr = errors.New("put job failed")
	service.MarkDelivered(context.Background(), "agent-1", job.ID, now.Add(5*time.Second))

	list := service.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(list), 1)
	}
	if list[0].Status != jobs.StatusRunning {
		t.Fatalf("jobs[0].Status = %q, want %q", list[0].Status, jobs.StatusRunning)
	}
	if list[0].Targets[0].Status != jobs.TargetStatusSent {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", list[0].Targets[0].Status, jobs.TargetStatusSent)
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

	first := jobs.NewServiceWithStore(store)
	job, err := first.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "pending-after-restore",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	restored := jobs.NewServiceWithStore(store)
	restored.SetNow(func() time.Time { return now.Add(time.Minute) })
	pending := restored.PendingForAgent(context.Background(), "agent-1", retryAfter)
	if len(pending) != 1 {
		t.Fatalf("len(PendingForAgent) = %d, want %d", len(pending), 1)
	}
	if pending[0].ID != job.ID {
		t.Fatalf("pending[0].ID = %q, want %q", pending[0].ID, job.ID)
	}
}

func TestServiceListPersistsExpiredQueuedJobsAcrossRestart(t *testing.T) {
	now := time.Date(2026, time.March, 19, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := jobs.NewServiceWithStore(store)
	job, err := first.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "persist-expired",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	first.SetNow(func() time.Time { return now.Add(2 * time.Minute) })
	list := first.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(list), 1)
	}
	if list[0].Status != jobs.StatusExpired {
		t.Fatalf("jobs[0].Status = %q, want %q", list[0].Status, jobs.StatusExpired)
	}
	if list[0].Targets[0].Status != jobs.TargetStatusExpired {
		t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", list[0].Targets[0].Status, jobs.TargetStatusExpired)
	}

	restored := jobs.NewServiceWithStore(store)
	restoredList := restored.List()
	if len(restoredList) != 1 {
		t.Fatalf("len(restored.List()) = %d, want %d", len(restoredList), 1)
	}
	if restoredList[0].ID != job.ID {
		t.Fatalf("restored.List()[0].ID = %q, want %q", restoredList[0].ID, job.ID)
	}
	if restoredList[0].Status != jobs.StatusExpired {
		t.Fatalf("restored.List()[0].Status = %q, want %q", restoredList[0].Status, jobs.StatusExpired)
	}
	if restoredList[0].Targets[0].Status != jobs.TargetStatusExpired {
		t.Fatalf("restored.List()[0].Targets[0].Status = %q, want %q", restoredList[0].Targets[0].Status, jobs.TargetStatusExpired)
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
	service := jobs.NewServiceWithStore(store)
	expiredJob, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-expired"},
		TTL:            time.Minute,
		IdempotencyKey: "expired-for-list-blocking",
		ActorID:        "user-1",
	}, baseNow.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("Enqueue(expired) error = %v", err)
	}
	liveJob, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-live"},
		TTL:            time.Hour,
		IdempotencyKey: "live-for-list-blocking",
		ActorID:        "user-1",
	}, baseNow)
	if err != nil {
		t.Fatalf("Enqueue(live) error = %v", err)
	}
	service.SetNow(func() time.Time { return baseNow })

	putJobStarted := make(chan struct{})
	releasePutJob := make(chan struct{})
	store.blockNextPutJob(putJobStarted, releasePutJob)

	listDone := make(chan []jobs.Job, 1)
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
		service.MarkDelivered(context.Background(), "agent-live", liveJob.ID, baseNow.Add(10*time.Second))
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

	list := service.List()
	if len(list) != 2 {
		t.Fatalf("len(List()) after unblock = %d, want %d", len(list), 2)
	}
	var foundExpired bool
	for _, j := range list {
		if j.ID != expiredJob.ID {
			continue
		}
		foundExpired = true
		if j.Status != jobs.StatusExpired {
			t.Fatalf("expired job status = %q, want %q", j.Status, jobs.StatusExpired)
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
	service := jobs.NewServiceWithStore(store)
	service.SetNow(func() time.Time { return now.Add(10 * time.Second) })
	job, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
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
		service.MarkDelivered(context.Background(), "agent-1", job.ID, now.Add(5*time.Second))
		close(markDone)
	}()

	select {
	case <-putJobStarted:
	case <-time.After(time.Second):
		t.Fatal("PutJob() did not block, want blocked persistence")
	}

	listDone := make(chan []jobs.Job, 1)
	go func() {
		listDone <- service.List()
	}()

	select {
	case listedJobs := <-listDone:
		if len(listedJobs) != 1 {
			t.Fatalf("len(List()) = %d, want %d", len(listedJobs), 1)
		}
		if listedJobs[0].Status != jobs.StatusRunning {
			t.Fatalf("jobs[0].Status = %q, want %q", listedJobs[0].Status, jobs.StatusRunning)
		}
		if listedJobs[0].Targets[0].Status != jobs.TargetStatusSent {
			t.Fatalf("jobs[0].Targets[0].Status = %q, want %q", listedJobs[0].Targets[0].Status, jobs.TargetStatusSent)
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
	service := jobs.NewServiceWithStore(store)
	service.SetNow(func() time.Time { return now.Add(10 * time.Second) })
	job, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
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
		service.MarkDelivered(context.Background(), "agent-1", job.ID, now.Add(5*time.Second))
		close(markDone)
	}()

	select {
	case <-putJobStarted:
	case <-time.After(time.Second):
		t.Fatal("PutJob() did not block, want out-of-order write setup")
	}

	service.RecordResult(context.Background(), "agent-1", job.ID, false, "failed", "", now.Add(6*time.Second))

	close(releasePutJob)

	select {
	case <-markDone:
	case <-time.After(time.Second):
		t.Fatal("MarkDelivered() did not complete after persistence release")
	}

	restored := jobs.NewServiceWithStore(sqliteStore)
	restoredList := restored.List()
	if len(restoredList) != 1 {
		t.Fatalf("len(restored.List()) = %d, want %d", len(restoredList), 1)
	}
	if restoredList[0].Status != jobs.StatusFailed {
		t.Fatalf("restored.List()[0].Status = %q, want %q", restoredList[0].Status, jobs.StatusFailed)
	}
	if len(restoredList[0].Targets) != 1 {
		t.Fatalf("len(restored.List()[0].Targets) = %d, want %d", len(restoredList[0].Targets), 1)
	}
	if restoredList[0].Targets[0].Status != jobs.TargetStatusFailed {
		t.Fatalf("restored.List()[0].Targets[0].Status = %q, want %q", restoredList[0].Targets[0].Status, jobs.TargetStatusFailed)
	}
	if restoredList[0].Targets[0].ResultText != "failed" {
		t.Fatalf("restored.List()[0].Targets[0].ResultText = %q, want %q", restoredList[0].Targets[0].ResultText, "failed")
	}
}

func TestNewServiceWithStoreRecordsRestoreError(t *testing.T) {
	store := &failingJobStore{
		listJobsErr: errors.New("list jobs failed"),
	}

	service := jobs.NewServiceWithStore(store)

	if service.StartupError() == nil {
		t.Fatal("StartupError() = nil, want restore failure")
	}
}

// TestEnqueueReleasesLockDuringPersist verifies P2-PERF-04: Enqueue must not
// hold the jobs service mutex across the synchronous PutJob/PutJobTarget
// calls, otherwise every concurrent PendingForAgent is forced to wait for
// the DB round-trip.
func TestEnqueueReleasesLockDuringPersist(t *testing.T) {
	now := time.Date(2026, time.April, 17, 10, 0, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &blockingJobStore{JobStore: sqliteStore}
	service := jobs.NewServiceWithStore(store)
	service.SetNow(func() time.Time { return now })

	putJobStarted := make(chan struct{})
	releasePutJob := make(chan struct{})
	store.blockNextPutJob(putJobStarted, releasePutJob)

	enqueueDone := make(chan error, 1)
	go func() {
		_, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
			Action:         jobs.ActionRuntimeReload,
			TargetAgentIDs: []string{"agent-1"},
			TTL:            time.Minute,
			IdempotencyKey: "p2-perf-04-slow-persist",
			ActorID:        "user-1",
		}, now)
		enqueueDone <- err
	}()

	select {
	case <-putJobStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("PutJob() did not start, want Enqueue to reach persist phase")
	}

	// While PutJob is parked, concurrent readers that only need the
	// in-memory state must not block.
	pendingDone := make(chan []jobs.Job, 1)
	go func() {
		pendingDone <- service.PendingForAgent(context.Background(), "agent-1", time.Second)
	}()
	select {
	case pending := <-pendingDone:
		// The in-flight Enqueue has not yet published into the in-memory maps,
		// so this PendingForAgent call correctly sees zero jobs — the key
		// point is it did NOT block on the stalled PutJob.
		if len(pending) != 0 {
			t.Fatalf("PendingForAgent() = %d jobs, want 0 while persist in-flight", len(pending))
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("PendingForAgent() blocked on Enqueue's in-flight persist — lock not released")
	}

	// Other read-only queries should also proceed.
	depthDone := make(chan int, 1)
	go func() {
		depthDone <- service.QueueDepth()
	}()
	select {
	case <-depthDone:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("QueueDepth() blocked on Enqueue's in-flight persist")
	}

	close(releasePutJob)

	select {
	case err := <-enqueueDone:
		if err != nil {
			t.Fatalf("Enqueue() error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue() did not complete after PutJob release")
	}

	// After the persist completes, the job is visible to PendingForAgent.
	pending := service.PendingForAgent(context.Background(), "agent-1", time.Second)
	if len(pending) != 1 {
		t.Fatalf("PendingForAgent() after persist = %d, want 1", len(pending))
	}
}

// TestEnqueueDuplicateKeyRejectedDuringOutOfLockWindow verifies that while an
// Enqueue is in its out-of-lock persist phase a second Enqueue with the same
// idempotency key is rejected with ErrDuplicateIdempotencyKey — the key map
// reservation guards the race.
func TestEnqueueDuplicateKeyRejectedDuringOutOfLockWindow(t *testing.T) {
	now := time.Date(2026, time.April, 17, 10, 0, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &blockingJobStore{JobStore: sqliteStore}
	service := jobs.NewServiceWithStore(store)

	putJobStarted := make(chan struct{})
	releasePutJob := make(chan struct{})
	store.blockNextPutJob(putJobStarted, releasePutJob)

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
			Action:         jobs.ActionRuntimeReload,
			TargetAgentIDs: []string{"agent-1"},
			TTL:            time.Minute,
			IdempotencyKey: "p2-perf-04-dup-key",
			ActorID:        "user-1",
		}, now)
		firstDone <- err
	}()

	select {
	case <-putJobStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first Enqueue did not reach persist phase")
	}

	// Second Enqueue with the same key — must be rejected immediately even
	// though the first one has not completed persist yet.
	_, err = service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-2"},
		TTL:            time.Minute,
		IdempotencyKey: "p2-perf-04-dup-key",
		ActorID:        "user-1",
	}, now)
	if !errors.Is(err, jobs.ErrDuplicateIdempotencyKey) {
		t.Fatalf("second Enqueue err = %v, want ErrDuplicateIdempotencyKey", err)
	}

	close(releasePutJob)

	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first Enqueue err = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first Enqueue did not complete after release")
	}

	// After the in-flight call completes the key is still reserved by the
	// first job — a third Enqueue must also see the duplicate.
	_, err = service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-3"},
		TTL:            time.Minute,
		IdempotencyKey: "p2-perf-04-dup-key",
		ActorID:        "user-1",
	}, now)
	if !errors.Is(err, jobs.ErrDuplicateIdempotencyKey) {
		t.Fatalf("third Enqueue err = %v, want ErrDuplicateIdempotencyKey", err)
	}

	list := service.List()
	if len(list) != 1 {
		t.Fatalf("len(List()) = %d, want 1 (exactly one winner)", len(list))
	}
}

// TestEnqueueDuplicateKeyConcurrentExactlyOneWins launches many parallel
// Enqueue calls with the same idempotency key and asserts exactly one
// succeeds, the rest see ErrDuplicateIdempotencyKey, and no tentative
// reservation leaks in s.keys.
func TestEnqueueDuplicateKeyConcurrentExactlyOneWins(t *testing.T) {
	now := time.Date(2026, time.April, 17, 10, 0, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	service := jobs.NewServiceWithStore(sqliteStore)

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	results := make(chan error, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
				Action:         jobs.ActionRuntimeReload,
				TargetAgentIDs: []string{"agent-1"},
				TTL:            time.Minute,
				IdempotencyKey: "p2-perf-04-race",
				ActorID:        "user-1",
			}, now)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	duplicates := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, jobs.ErrDuplicateIdempotencyKey):
			duplicates++
		default:
			t.Fatalf("unexpected Enqueue error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want 1", successes)
	}
	if duplicates != workers-1 {
		t.Fatalf("duplicates = %d, want %d", duplicates, workers-1)
	}
}

// TestEnqueuePersistFailureRollsBack verifies P2-PERF-04: when PutJob fails
// the tentative idempotency-key reservation is rolled back, so the caller
// (or a retry) can use the same key again and no ghost job lingers in the
// in-memory maps.
func TestEnqueuePersistFailureRollsBack(t *testing.T) {
	now := time.Date(2026, time.April, 17, 10, 0, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	putErr := errors.New("simulated put-job failure")
	store := &failingJobStore{JobStore: sqliteStore, putJobErr: putErr}
	service := jobs.NewServiceWithStore(store)

	_, err = service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "p2-perf-04-rollback",
		ActorID:        "user-1",
	}, now)
	if !errors.Is(err, putErr) {
		t.Fatalf("Enqueue() err = %v, want %v", err, putErr)
	}

	// No job should be visible in the in-memory state.
	if list := service.List(); len(list) != 0 {
		t.Fatalf("len(List()) = %d, want 0 after rollback", len(list))
	}
	if depth := service.QueueDepth(); depth != 0 {
		t.Fatalf("QueueDepth() = %d, want 0 after rollback", depth)
	}
	if pending := service.PendingForAgent(context.Background(), "agent-1", time.Second); len(pending) != 0 {
		t.Fatalf("PendingForAgent() = %d, want 0 after rollback", len(pending))
	}

	// The idempotency-key reservation must be released — a retry with the
	// same key should now succeed once the store is healthy again.
	store.putJobErr = nil
	job, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "p2-perf-04-rollback",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("retry Enqueue() err = %v, want nil", err)
	}
	if job.ID == "" {
		t.Fatalf("retry Enqueue() returned empty job ID")
	}
}

// TestAcknowledgedJobsAreRedispatchedAfterRestart verifies the P2-LOG-05
// (L-14) fix: when the CP restarts and rebuilds agentJobs from the store,
// acknowledged targets must be re-dispatchable so a second agent restart
// (which drops the agent's in-flight queue) does not leave the job wedged
// forever.
func TestAcknowledgedJobsAreRedispatchedAfterRestart(t *testing.T) {
	const retryAfter = 30 * time.Second
	now := time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := jobs.NewServiceWithStore(store)
	first.SetNow(func() time.Time { return now })
	job, err := first.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "ack-redispatch",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	first.MarkDelivered(context.Background(), "agent-1", job.ID, now.Add(time.Second))
	first.MarkAcknowledged(context.Background(), "agent-1", job.ID, now.Add(2*time.Second))

	// Sanity: during normal operation, acked targets are pruned from the
	// agent index so PendingForAgent does not re-dispatch the live job.
	if pending := first.PendingForAgent(context.Background(), "agent-1", retryAfter); len(pending) != 0 {
		t.Fatalf("len(first.PendingForAgent) = %d, want 0 for acked live job", len(pending))
	}

	// Simulate dual restart: new service instance rebuilds from the same
	// store, clock advances enough that the ack retryAfter window has
	// elapsed (mirrors real "CP restarted, then agent restarted" timing).
	restored := jobs.NewServiceWithStore(store)
	restored.SetNow(func() time.Time { return now.Add(5 * time.Minute) })

	pending := restored.PendingForAgent(context.Background(), "agent-1", retryAfter)
	if len(pending) != 1 {
		t.Fatalf("len(restored.PendingForAgent) = %d, want 1 (ack should be redispatchable after restart)", len(pending))
	}
	if pending[0].ID != job.ID {
		t.Fatalf("pending[0].ID = %q, want %q", pending[0].ID, job.ID)
	}
	if pending[0].Targets[0].Status != jobs.TargetStatusAcknowledged {
		t.Fatalf("pending[0].Targets[0].Status = %q, want %q (status history preserved)", pending[0].Targets[0].Status, jobs.TargetStatusAcknowledged)
	}
}

// TestEnqueueRetryAfterTransientStoreError verifies that when PutJob fails
// the idempotency-key reservation is released so the caller can retry the
// SAME key. Moved to package jobs_test to avoid the sqlite ↔ jobs import
// cycle introduced when storage/sqlite/jobs_repository.go imports package jobs.
func TestEnqueueRetryAfterTransientStoreError(t *testing.T) {
	now := time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer sqliteStore.Close()

	store := &failingJobStore{JobStore: sqliteStore}
	service := jobs.NewServiceWithStore(store)

	store.putJobErr = errors.New("transient")

	if _, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "retry-key",
		ActorID:        "user-1",
	}, now); err == nil {
		t.Fatal("Enqueue first attempt err = nil, want transient error")
	}

	// Retry with the same idempotency key — must succeed because the
	// reservation was rolled back.
	store.putJobErr = nil
	job, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "retry-key",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue retry err = %v, want nil (key must have been released)", err)
	}
	if job.IdempotencyKey != "retry-key" {
		t.Fatalf("retry job key = %q, want %q", job.IdempotencyKey, "retry-key")
	}
	if job.Status != jobs.StatusQueued {
		t.Fatalf("retry job status = %q, want queued", job.Status)
	}
}

// ---------------------------------------------------------------------------
// C3: MetricsSink — persist failure counter
// ---------------------------------------------------------------------------

type recordingJobMetricsSink struct {
	mu       sync.Mutex
	failures int
}

func (r *recordingJobMetricsSink) ObserveJobPersistFailure() {
	r.mu.Lock()
	r.failures++
	r.mu.Unlock()
}

func (r *recordingJobMetricsSink) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failures
}

// TestServicePersistFailureNotifiesMetricsSink (C3): a write-behind
// persist failure must increment the injected metrics sink — slog-only
// surfacing leaves a wedged DB invisible to operators.
func TestServicePersistFailureNotifiesMetricsSink(t *testing.T) {
	now := time.Now().UTC()
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &failingJobStore{JobStore: sqliteStore}
	service := jobs.NewServiceWithStore(store)
	sink := &recordingJobMetricsSink{}
	service.SetMetricsSink(sink)

	job, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "persist-failure-metric",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	store.putJobErr = errors.New("put job failed")
	service.MarkDelivered(context.Background(), "agent-1", job.ID, now.Add(5*time.Second))

	if got := sink.count(); got == 0 {
		t.Fatalf("sink failures = %d, want >= 1 after persist failure", got)
	}
}
