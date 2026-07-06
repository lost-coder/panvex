package server

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// TestConfigApplyBatchAdvanceAllSucceededFinalizesBatch drives a real
// all_at_once batch's two jobs to terminal-succeeded via the jobs service's
// own RecordResult API (least brittle: no fake/mock of *jobs.Service, which
// is a concrete type with unexported state), then asserts
// advanceConfigApplyBatch folds that into the persisted batch/target rows.
// Re-running on the now-terminal batch must be a no-op (idempotency).
func TestConfigApplyBatchAdvanceAllSucceededFinalizesBatch(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "advance-succeed-group", time.Time{})
	const agentA = "agent-advance-a"
	const agentB = "agent-advance-b"
	srv.seedLiveAgent(Agent{ID: agentA, FleetGroupID: groupID})
	srv.seedLiveAgent(Agent{ID: agentB, FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	ctx := context.Background()
	batchID, err := srv.createConfigApplyBatch(ctx, "tester", groupID, []string{agentA, agentB})
	if err != nil {
		t.Fatalf("createConfigApplyBatch() error = %v", err)
	}
	batch, targets, err := srv.store.GetConfigApplyBatch(ctx, batchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s): %v", batchID, err)
	}
	for _, tgt := range targets {
		if !srv.jobs.RecordResult(ctx, tgt.AgentID, tgt.JobID, true, "ok", "", time.Now()) {
			t.Fatalf("RecordResult(success) for %s returned false", tgt.AgentID)
		}
	}

	if err := srv.advanceConfigApplyBatch(ctx, batch); err != nil {
		t.Fatalf("advanceConfigApplyBatch() error = %v", err)
	}

	gotBatch, gotTargets, err := srv.store.GetConfigApplyBatch(ctx, batchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s) after advance: %v", batchID, err)
	}
	if gotBatch.Status != storage.ConfigApplyBatchStatusSucceeded {
		t.Fatalf("batch status = %q, want %q", gotBatch.Status, storage.ConfigApplyBatchStatusSucceeded)
	}
	for _, tgt := range gotTargets {
		if tgt.Status != storage.ConfigApplyTargetStatusSucceeded {
			t.Fatalf("target %s status = %q, want %q", tgt.AgentID, tgt.Status, storage.ConfigApplyTargetStatusSucceeded)
		}
	}

	// Idempotent: re-running on the now-terminal batch must not error and
	// must not regress the already-terminal status.
	if err := srv.advanceConfigApplyBatch(ctx, gotBatch); err != nil {
		t.Fatalf("advanceConfigApplyBatch() second call error = %v", err)
	}
	againBatch, _, err := srv.store.GetConfigApplyBatch(ctx, batchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s) after second advance: %v", batchID, err)
	}
	if againBatch.Status != storage.ConfigApplyBatchStatusSucceeded {
		t.Fatalf("batch status after second advance = %q, want %q", againBatch.Status, storage.ConfigApplyBatchStatusSucceeded)
	}
}

// TestConfigApplyBatchAdvanceOneFailedFinalizesBatchFailed mirrors the
// partial-failure status test in http_config_apply_test.go, but drives the
// persisted batch through advanceConfigApplyBatch instead of the legacy
// job-id status endpoint: one agent's job succeeds, the other fails, and the
// batch must finalize as failed with the failing target marked failed.
func TestConfigApplyBatchAdvanceOneFailedFinalizesBatchFailed(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "advance-fail-group", time.Time{})
	const okAgent = "agent-advance-ok"
	const failAgent = "agent-advance-fail"
	srv.seedLiveAgent(Agent{ID: okAgent, FleetGroupID: groupID})
	srv.seedLiveAgent(Agent{ID: failAgent, FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	ctx := context.Background()
	batchID, err := srv.createConfigApplyBatch(ctx, "tester", groupID, []string{okAgent, failAgent})
	if err != nil {
		t.Fatalf("createConfigApplyBatch() error = %v", err)
	}
	batch, targets, err := srv.store.GetConfigApplyBatch(ctx, batchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s): %v", batchID, err)
	}
	for _, tgt := range targets {
		success := tgt.AgentID == okAgent
		msg := "ok"
		if !success {
			msg = "health check failed"
		}
		if !srv.jobs.RecordResult(ctx, tgt.AgentID, tgt.JobID, success, msg, "", time.Now()) {
			t.Fatalf("RecordResult for %s returned false", tgt.AgentID)
		}
	}

	if err := srv.advanceConfigApplyBatch(ctx, batch); err != nil {
		t.Fatalf("advanceConfigApplyBatch() error = %v", err)
	}

	gotBatch, gotTargets, err := srv.store.GetConfigApplyBatch(ctx, batchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s) after advance: %v", batchID, err)
	}
	if gotBatch.Status != storage.ConfigApplyBatchStatusFailed {
		t.Fatalf("batch status = %q, want %q", gotBatch.Status, storage.ConfigApplyBatchStatusFailed)
	}
	for _, tgt := range gotTargets {
		want := storage.ConfigApplyTargetStatusSucceeded
		if tgt.AgentID == failAgent {
			want = storage.ConfigApplyTargetStatusFailed
		}
		if tgt.Status != want {
			t.Fatalf("target %s status = %q, want %q", tgt.AgentID, tgt.Status, want)
		}
	}
}

// TestConfigApplyBatchAdvanceNonTerminalStaysRunning asserts a batch whose
// target job has not yet reached a terminal state is left alone: no
// premature finalize. The job is nudged to "sent" via MarkDelivered (the
// same transition the real agent stream drives) so the target is
// legitimately mid-flight rather than merely unqueued.
func TestConfigApplyBatchAdvanceNonTerminalStaysRunning(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "advance-pending-group", time.Time{})
	const agentID = "agent-advance-pending"
	srv.seedLiveAgent(Agent{ID: agentID, FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	ctx := context.Background()
	batchID, err := srv.createConfigApplyBatch(ctx, "tester", groupID, []string{agentID})
	if err != nil {
		t.Fatalf("createConfigApplyBatch() error = %v", err)
	}
	batch, targets, err := srv.store.GetConfigApplyBatch(ctx, batchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s): %v", batchID, err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(targets))
	}
	srv.jobs.MarkDelivered(ctx, agentID, targets[0].JobID, time.Now())

	if err := srv.advanceConfigApplyBatch(ctx, batch); err != nil {
		t.Fatalf("advanceConfigApplyBatch() error = %v", err)
	}

	gotBatch, gotTargets, err := srv.store.GetConfigApplyBatch(ctx, batchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s) after advance: %v", batchID, err)
	}
	if gotBatch.Status != storage.ConfigApplyBatchStatusRunning {
		t.Fatalf("batch status = %q, want %q (must not finalize prematurely)", gotBatch.Status, storage.ConfigApplyBatchStatusRunning)
	}
	if len(gotTargets) != 1 || gotTargets[0].Status != storage.ConfigApplyTargetStatusRunning {
		t.Fatalf("target status = %+v, want running (non-terminal)", gotTargets)
	}
}

// TestConfigApplyBatchAdvanceJobEvictedBeforePersistFinalizesFailed is the
// regression test for audit 2026-07-02 #2: a config.apply job that failed
// and was then evicted from the in-memory jobs store (terminal-key TTL,
// jobs.Service.PruneKeys) BEFORE the batch poll-worker updated the batch
// target row used to be resolved as succeeded by configApplyJobStatus's
// missing-job default — finalizing a failed rollout as a successful batch.
// The batch must finalize FAILED. P8.1 (audit #24): with durable history
// the evicted job is read back from the jobs table (its terminal FAILED row
// outlives the in-memory eviction), so the target now carries the agent's
// REAL failure reason ("health check failed") instead of the generic
// job-lost fallback — the fallback fires only when the row is absent from
// BOTH memory and store.
func TestConfigApplyBatchAdvanceJobEvictedBeforePersistFinalizesFailed(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "advance-evicted-group", time.Time{})
	const agentID = "agent-advance-evicted"
	srv.seedLiveAgent(Agent{ID: agentID, FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	ctx := context.Background()
	batchID, err := srv.createConfigApplyBatch(ctx, "tester", groupID, []string{agentID})
	if err != nil {
		t.Fatalf("createConfigApplyBatch() error = %v", err)
	}
	batch, targets, err := srv.store.GetConfigApplyBatch(ctx, batchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s): %v", batchID, err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(targets))
	}

	// The job FAILS on the agent (terminal in the jobs store)...
	if !srv.jobs.RecordResult(ctx, agentID, targets[0].JobID, false, "health check failed", "", time.Now()) {
		t.Fatalf("RecordResult(failure) returned false")
	}
	// ...and is evicted before the batch worker ever ticked: advance the
	// jobs clock past the terminal-key TTL and prune. The persisted batch
	// target still says "running".
	srv.jobs.SetNow(func() time.Time { return time.Now().Add(2 * time.Hour) })
	if evicted := srv.jobs.PruneKeys(time.Hour); evicted == 0 {
		t.Fatalf("PruneKeys() evicted 0 jobs, want >= 1")
	}
	if _, ok := srv.jobs.Get(targets[0].JobID); ok {
		t.Fatalf("job %s still resident after prune — eviction setup broken", targets[0].JobID)
	}

	if err := srv.advanceConfigApplyBatch(ctx, batch); err != nil {
		t.Fatalf("advanceConfigApplyBatch() error = %v", err)
	}

	gotBatch, gotTargets, err := srv.store.GetConfigApplyBatch(ctx, batchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s) after advance: %v", batchID, err)
	}
	if gotBatch.Status != storage.ConfigApplyBatchStatusFailed {
		t.Fatalf("batch status = %q, want %q (lost job must not read as success)", gotBatch.Status, storage.ConfigApplyBatchStatusFailed)
	}
	if len(gotTargets) != 1 || gotTargets[0].Status != storage.ConfigApplyTargetStatusFailed {
		t.Fatalf("target = %+v, want status %q", gotTargets, storage.ConfigApplyTargetStatusFailed)
	}
	// P8.1: durable history surfaces the agent's real failure reason read
	// back from the jobs table, not the generic job-lost fallback.
	if gotTargets[0].Message != "health check failed" {
		t.Fatalf("target message = %q, want the agent's real reason %q", gotTargets[0].Message, "health check failed")
	}
}

// evictedRestoreJobStore simulates "rows are durable, memory is cold": a
// jobs.Service constructed over it restores NOTHING into memory (ListJobs /
// ListAllJobTargets are empty) while point reads (GetJob / ListJobTargets)
// hit the real store. This is exactly the state after PruneKeys evicted a
// terminal job, without having to manipulate the service clock.
type evictedRestoreJobStore struct{ storage.JobStore }

func (evictedRestoreJobStore) ListJobs(context.Context) ([]storage.JobRecord, error) {
	return nil, nil
}

func (evictedRestoreJobStore) ListAllJobTargets(context.Context) ([]storage.JobTargetRecord, error) {
	return nil, nil
}

// TestConfigApplyJobStatusReadsEvictedJobFromStore: P8.1 — a config.apply
// job whose terminal result reached the store but which is no longer
// resident in memory must resolve to its REAL outcome, not to the P1
// job-lost fallback.
func TestConfigApplyJobStatusReadsEvictedJobFromStore(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "status-store-group", time.Time{})
	const agentID = "agent-status-store"
	srv.seedLiveAgent(Agent{ID: agentID, FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	ctx := context.Background()
	batchID, err := srv.createConfigApplyBatch(ctx, "tester", groupID, []string{agentID})
	if err != nil {
		t.Fatalf("createConfigApplyBatch() error = %v", err)
	}
	_, targets, err := srv.store.GetConfigApplyBatch(ctx, batchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s): %v", batchID, err)
	}
	if !srv.jobs.RecordResult(ctx, agentID, targets[0].JobID, false, "agent exploded", "", time.Now()) {
		t.Fatal("RecordResult returned false")
	}

	// «Рестарт с холодной памятью»: свежий сервис поверх того же store.
	srv.jobs = jobs.NewServiceWithStore(ctx, evictedRestoreJobStore{JobStore: srv.store})
	if _, ok := srv.jobs.Get(targets[0].JobID); ok {
		t.Fatal("job unexpectedly resident in memory")
	}

	status, message := srv.configApplyJobStatus(ctx, targets[0].JobID, agentID)
	if status != applyStatusFailed {
		t.Fatalf("status = %q, want %q", status, applyStatusFailed)
	}
	if message != "agent exploded" {
		t.Fatalf("message = %q, want the agent's own result text, not %q", message, configApplyMsgJobLost)
	}
}
