// package jobs_test uses the external test package to avoid the import cycle
// that arises when storage/sqlite/jobs_repository.go imports package jobs
// while the tests in package jobs import storage/sqlite. The one test that
// required package jobs (TestEnqueueRetryAfterTransientStoreError, which
// accessed the unexported failingJobStore) has been moved to service_test.go.
package jobs_test

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestExpireStaleSealsQueuedJob verifies the ExpireStale path: once a
// job's TTL has elapsed, ExpireStale must seal it as expired and the
// terminal-key bookkeeping must record the eviction timestamp so PruneKeys
// can later release the idempotency key.
//
// Existing TestServiceListProjectsExpiredQueuedJobsAsFailed covers the
// List-driven projection; this one drives the sealing through the explicit
// ExpireStale entry-point and asserts the key-bookkeeping side effect (S27 T2).
func TestExpireStaleSealsQueuedJob(t *testing.T) {
	start := time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	currentNow := start
	service := jobs.NewService()
	service.SetNow(func() time.Time { return currentNow })

	if _, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "expire-key",
		ActorID:        "user-1",
	}, currentNow); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Pre-TTL: ExpireStale is a no-op.
	currentNow = start.Add(30 * time.Second)
	service.ExpireStale(context.Background())
	depth := service.QueueDepth()
	if depth != 1 {
		t.Fatalf("QueueDepth pre-TTL = %d, want 1 (still queued)", depth)
	}

	// Post-TTL: ExpireStale flips status to Expired.
	currentNow = start.Add(2 * time.Minute)
	service.ExpireStale(context.Background())

	list := service.List()
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}
	if list[0].Status != jobs.StatusExpired {
		t.Fatalf("expired job status = %q, want %q", list[0].Status, jobs.StatusExpired)
	}

	// PruneKeys with TTL > age must NOT evict — terminal-at is recent.
	if evicted := service.PruneKeys(time.Hour); evicted != 0 {
		t.Fatalf("PruneKeys(1h) post-expire = %d, want 0", evicted)
	}
	// Advance the clock past PruneKeys TTL — the key must now evict,
	// proving ExpireStale recorded the terminal timestamp.
	currentNow = start.Add(3 * time.Hour)
	if evicted := service.PruneKeys(time.Hour); evicted != 1 {
		t.Fatalf("PruneKeys(1h) past TTL = %d, want 1 (terminal-at must have been recorded)", evicted)
	}
}

// TestRecordResultIdempotentForSealedTarget verifies that recording a
// result against a target that has already reached a terminal target
// status (Succeeded / Expired / Failed) is a benign no-op for the
// "expired" case and keeps the original verdict for the
// already-succeeded case. RecordResult only short-circuits on Expired
// per the production code; this guards against a regression that would
// allow a late-arriving result to flip a sealed-expired target's
// verdict (S27 T2).
func TestRecordResultIdempotentForSealedTarget(t *testing.T) {
	start := time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	currentNow := start
	service := jobs.NewService()
	service.SetNow(func() time.Time { return currentNow })

	job, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "seal-key",
		ActorID:        "user-1",
	}, currentNow)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Walk the clock past TTL so ExpireStale flips the target to Expired
	// (not via RecordResult).
	currentNow = start.Add(2 * time.Minute)
	service.ExpireStale(context.Background())

	// Now a late RecordResult arrives — must NOT re-flip the expired
	// target's status. updateTarget returns true (job exists), but the
	// result mutation must be a no-op for the expired target.
	currentNow = start.Add(3 * time.Minute)
	_ = service.RecordResult(context.Background(), "agent-1", job.ID, true, "late-ok", "", currentNow)

	got, ok := service.Get(job.ID)
	if !ok {
		t.Fatal("Get after late RecordResult: not found")
	}
	if got.Targets[0].Status != jobs.TargetStatusExpired {
		t.Fatalf("target status = %q, want %q (late RecordResult must not unseal)",
			got.Targets[0].Status, jobs.TargetStatusExpired)
	}
	if got.Status != jobs.StatusExpired {
		t.Fatalf("job status = %q, want %q", got.Status, jobs.StatusExpired)
	}
}

// TestConcurrentEnqueueListRecordResult exercises the RWMutex correctness
// (P-6): N goroutines run Enqueue / ListWithContext / RecordResult on
// disjoint keys concurrently. Must not race, must not deadlock, must not
// drop or duplicate jobs. Run with -race to catch any unsynchronised
// access (S27 T2).
func TestConcurrentEnqueueListRecordResult(t *testing.T) {
	const enqueuers = 8
	const perEnq = 25

	start := time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	currentNow := start
	var nowMu sync.RWMutex
	service := jobs.NewService()
	service.SetNow(func() time.Time {
		nowMu.RLock()
		defer nowMu.RUnlock()
		return currentNow
	})

	var wg sync.WaitGroup
	var enqErr atomic.Int64
	jobIDs := make(chan string, enqueuers*perEnq)

	// Enqueuers.
	for w := 0; w < enqueuers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perEnq; i++ {
				key := keyFor(w, i)
				job, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
					Action:         jobs.ActionRuntimeReload,
					TargetAgentIDs: []string{"agent-1"},
					TTL:            time.Hour,
					IdempotencyKey: key,
					ActorID:        "user-1",
				}, start)
				if err != nil {
					enqErr.Add(1)
					return
				}
				jobIDs <- job.ID
			}
		}(w)
	}

	// Concurrent readers + result-recorders.
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = service.ListWithContext(context.Background())
				_ = service.QueueDepth()
			}
		}()
	}

	wg.Wait()
	close(jobIDs)
	close(stop)
	readers.Wait()

	if enqErr.Load() != 0 {
		t.Fatalf("Enqueue failures = %d, want 0", enqErr.Load())
	}

	// Drain succeeded job IDs and seal each target via RecordResult.
	for id := range jobIDs {
		ok := service.RecordResult(context.Background(), "agent-1", id, true, "ok", "", start)
		if !ok {
			t.Fatalf("RecordResult(%q) ok = false, want true", id)
		}
	}

	list := service.List()
	if len(list) != enqueuers*perEnq {
		t.Fatalf("List len after seals = %d, want %d (jobs must not be dropped)", len(list), enqueuers*perEnq)
	}
	for _, j := range list {
		if j.Status != jobs.StatusSucceeded {
			t.Fatalf("job %q status = %q, want %q", j.ID, j.Status, jobs.StatusSucceeded)
		}
	}
	if d := service.QueueDepth(); d != 0 {
		t.Fatalf("QueueDepth post-seal = %d, want 0 (all jobs reached terminal)", d)
	}
}

func keyFor(worker, iter int) string {
	return "race-" + itoa(worker) + "-" + itoa(iter)
}

// itoa is intentionally hand-rolled to avoid pulling strconv into the
// table-test loop (also useful in -race mode where allocation churn can
// mask races on the unrelated atomic).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TestRestoreRebuildsLatestSucceededByClient verifies P-4 restart parity:
// after a control-plane restart the LatestSucceededWithContext index
// must reflect the persisted succeeded client.* job (S27 T2).
func TestRestoreRebuildsLatestSucceededByClient(t *testing.T) {
	now := time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	clientID := "client-restored"
	payload := `{"client_id":"` + clientID + `","name":"alice"}`

	first := jobs.NewServiceWithStore(context.Background(), store)
	first.SetNow(func() time.Time { return now })
	job, err := first.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionClientCreate,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "client-create-restore",
		ActorID:        "user-1",
		PayloadJSON:    payload,
	}, now)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	first.MarkDelivered(context.Background(), "agent-1", job.ID, now)
	first.MarkAcknowledged(context.Background(), "agent-1", job.ID, now)
	if !first.RecordResult(context.Background(), "agent-1", job.ID, true, "ok", `{"connection_links":["link-a"]}`, now) {
		t.Fatal("RecordResult: false")
	}

	// New service, same store — must re-hydrate the index.
	restored := jobs.NewServiceWithStore(context.Background(), store)
	restored.SetNow(func() time.Time { return now.Add(time.Minute) })
	if err := restored.StartupError(); err != nil {
		t.Fatalf("StartupError: %v", err)
	}

	got, ok := restored.LatestSucceededWithContext(context.Background(), clientID)
	if !ok || got == nil {
		t.Fatal("LatestSucceededWithContext after restart = miss, want hit")
	}
	if got.ID != job.ID {
		t.Fatalf("LatestSucceededWithContext.ID = %q, want %q", got.ID, job.ID)
	}
	if got.Status != jobs.StatusSucceeded {
		t.Fatalf("status = %q, want %q", got.Status, jobs.StatusSucceeded)
	}
	if len(got.Targets) != 1 || got.Targets[0].ResultJSON == "" {
		t.Fatalf("Targets[0].ResultJSON empty after restore — result-blob persistence regression")
	}
}

// TestLatestSucceededByClientMonotone verifies the index does NOT
// downgrade when an older succeeded client.* job arrives after a
// newer one (P-4 monotone-by-CreatedAt). Catches a regression where
// out-of-order MarkDelivered/RecordResult races flip the index back
// to a stale row (S27 T2).
func TestLatestSucceededByClientMonotone(t *testing.T) {
	clientID := "client-monotone"
	payload := `{"client_id":"` + clientID + `"}`

	now1 := time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	now2 := now1.Add(10 * time.Minute) // newer

	currentNow := now2
	service := jobs.NewService()
	service.SetNow(func() time.Time { return currentNow })

	// Enqueue+complete the NEWER job first (now2).
	jobNew, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionClientUpdate,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "k-new",
		ActorID:        "user-1",
		PayloadJSON:    payload,
	}, now2)
	if err != nil {
		t.Fatalf("Enqueue jobNew: %v", err)
	}
	service.MarkDelivered(context.Background(), "agent-1", jobNew.ID, now2)
	service.MarkAcknowledged(context.Background(), "agent-1", jobNew.ID, now2)
	if !service.RecordResult(context.Background(), "agent-1", jobNew.ID, true, "ok", "", now2) {
		t.Fatal("RecordResult jobNew: false")
	}

	// Now enqueue+complete the OLDER job (now1) — must NOT overwrite the
	// index. Pin clock to now1 for the Enqueue so CreatedAt is older,
	// but back to now2 for the rest so ExpireStale doesn't fire.
	currentNow = now1
	jobOld, err := service.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionClientUpdate,
		TargetAgentIDs: []string{"agent-2"},
		TTL:            time.Hour,
		IdempotencyKey: "k-old",
		ActorID:        "user-1",
		PayloadJSON:    payload,
	}, now1)
	if err != nil {
		t.Fatalf("Enqueue jobOld: %v", err)
	}
	currentNow = now2
	service.MarkDelivered(context.Background(), "agent-2", jobOld.ID, now2)
	service.MarkAcknowledged(context.Background(), "agent-2", jobOld.ID, now2)
	if !service.RecordResult(context.Background(), "agent-2", jobOld.ID, true, "ok", "", now2) {
		t.Fatal("RecordResult jobOld: false")
	}

	got, ok := service.LatestSucceededWithContext(context.Background(), clientID)
	if !ok {
		t.Fatal("LatestSucceededWithContext miss, want hit")
	}
	if got.ID != jobNew.ID {
		t.Fatalf("LatestSucceededWithContext.ID = %q, want %q (monotone-by-CreatedAt: older must not overwrite)", got.ID, jobNew.ID)
	}
}

// TestPersistedAcknowledgedTargetsRedispatchedAfterRestart verifies the
// in-memory vs persisted divergence guard: when CP and agent both
// restart between ack and result, the agent's runtime queue is empty.
// On restore, the acknowledged target must be re-flagged for delivery
// via PendingForAgent — otherwise the job is stuck forever (P2-LOG-05
// L-14). Mirrors TestAcknowledgedJobsAreRedispatchedAfterRestart but
// asserts the persistence-side invariant directly: the persisted record
// kept TargetStatusAcknowledged, AND restore re-indexes it as
// pending-for-agent (S27 T2).
func TestPersistedAcknowledgedTargetsRedispatchedAfterRestart(t *testing.T) {
	now := time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	first := jobs.NewServiceWithStore(context.Background(), store)
	first.SetNow(func() time.Time { return now })
	job, err := first.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Hour,
		IdempotencyKey: "ack-restore-key",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	first.MarkDelivered(context.Background(), "agent-1", job.ID, now)
	first.MarkAcknowledged(context.Background(), "agent-1", job.ID, now)

	// Verify persistence saw the acknowledged target.
	persisted, err := store.ListJobTargets(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("ListJobTargets: %v", err)
	}
	if len(persisted) != 1 || persisted[0].Status != string(jobs.TargetStatusAcknowledged) {
		t.Fatalf("persisted target status = %v, want %q", persisted, jobs.TargetStatusAcknowledged)
	}

	// Restart.
	restored := jobs.NewServiceWithStore(context.Background(), store)
	restored.SetNow(func() time.Time { return now.Add(time.Minute) })

	// PendingForAgent must re-include this job because acknowledged
	// targets are re-indexed in reindexAcknowledgedTargets at restore.
	pending := restored.PendingForAgent(context.Background(), "agent-1", time.Second)
	if len(pending) != 1 || pending[0].ID != job.ID {
		t.Fatalf("PendingForAgent after restart = %v, want one entry for %q", pending, job.ID)
	}
}
