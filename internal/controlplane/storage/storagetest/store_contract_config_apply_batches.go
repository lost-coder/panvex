package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// testFleetGroupIDB is a second deterministic UUIDv4 fixture, distinct from
// testFleetGroupID, used wherever a contract test needs two fleet groups to
// prove per-group isolation (e.g. ActiveConfigApplyBatchForGroup).
const testFleetGroupIDB = "00000000-0000-4000-a000-000000000002"

// runConfigApplyBatchContract exercises the config_apply_batches +
// config_apply_batch_targets tables: create (batch + targets, atomically) →
// get → per-target job/status updates → batch status transition → running
// listing → per-group active lookup → terminal prune with target cascade.
// RunStoreContract dispatches into it so each backend exercises the same
// coverage.
func runConfigApplyBatchContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("config apply batch lifecycle", func(t *testing.T) {
		st := open(t)
		defer st.Close()

		ctx := context.Background()
		base := time.Date(2026, time.June, 1, 8, 0, 0, 0, time.UTC)

		mustPutFleetGroup(t, ctx, st, testFleetGroupID, "Group A", base)
		mustPutFleetGroup(t, ctx, st, testFleetGroupIDB, "Group B", base)

		batch1 := storage.ConfigApplyBatchRecord{
			ID:               "batch-1",
			FleetGroupID:     testFleetGroupID,
			Mode:             storage.ConfigApplyBatchModeAllAtOnce,
			WaveSize:         1,
			ExpectedRevision: "rev-1",
			Status:           storage.ConfigApplyBatchStatusRunning,
			CreatedAt:        base,
			UpdatedAt:        base,
		}
		targets1 := []storage.ConfigApplyBatchTargetRecord{
			{BatchID: "batch-1", AgentID: "agent-2", WaveIndex: 0, Status: storage.ConfigApplyTargetStatusPending},
			{BatchID: "batch-1", AgentID: "agent-1", WaveIndex: 0, Status: storage.ConfigApplyTargetStatusPending},
		}
		if err := st.CreateConfigApplyBatch(ctx, batch1, targets1); err != nil {
			t.Fatalf("CreateConfigApplyBatch() error = %v", err)
		}

		gotBatch, gotTargets, err := st.GetConfigApplyBatch(ctx, "batch-1")
		if err != nil {
			t.Fatalf("GetConfigApplyBatch() error = %v", err)
		}
		assertBatchEqual(t, gotBatch, batch1)
		if len(gotTargets) != 2 {
			t.Fatalf("GetConfigApplyBatch() targets len = %d, want 2", len(gotTargets))
		}
		// Contract: targets come back ordered by wave_index then agent_id —
		// agent-1 before agent-2 even though they were inserted reversed.
		if gotTargets[0].AgentID != "agent-1" || gotTargets[1].AgentID != "agent-2" {
			t.Fatalf("GetConfigApplyBatch() target order = [%s, %s], want [agent-1, agent-2]",
				gotTargets[0].AgentID, gotTargets[1].AgentID)
		}
		for _, tg := range gotTargets {
			if tg.JobID != "" {
				t.Fatalf("target %s JobID = %q, want empty before enqueue", tg.AgentID, tg.JobID)
			}
			if tg.Status != storage.ConfigApplyTargetStatusPending {
				t.Fatalf("target %s Status = %q, want pending", tg.AgentID, tg.Status)
			}
		}

		// SetConfigApplyBatchTargetJob: wave enqueue populates job_id + status.
		if err := st.SetConfigApplyBatchTargetJob(ctx, "batch-1", "agent-1", "job-1", storage.ConfigApplyTargetStatusRunning); err != nil {
			t.Fatalf("SetConfigApplyBatchTargetJob() error = %v", err)
		}
		_, gotTargets, err = st.GetConfigApplyBatch(ctx, "batch-1")
		if err != nil {
			t.Fatalf("GetConfigApplyBatch() after job set error = %v", err)
		}
		agent1 := targetByAgent(t, gotTargets, "agent-1")
		if agent1.JobID != "job-1" || agent1.Status != storage.ConfigApplyTargetStatusRunning {
			t.Fatalf("agent-1 target = %+v, want JobID=job-1 Status=running", agent1)
		}
		agent2 := targetByAgent(t, gotTargets, "agent-2")
		if agent2.JobID != "" || agent2.Status != storage.ConfigApplyTargetStatusPending {
			t.Fatalf("agent-2 target = %+v, want untouched (empty job, pending)", agent2)
		}

		// UpdateConfigApplyBatchTargetStatus: status+message update, job id
		// untouched. agent-1's failure message must round-trip so it
		// survives eviction of the underlying job from the jobs store — the
		// whole point of persisting it on the target row.
		if err := st.UpdateConfigApplyBatchTargetStatus(ctx, "batch-1", "agent-1", storage.ConfigApplyTargetStatusFailed, "health check failed"); err != nil {
			t.Fatalf("UpdateConfigApplyBatchTargetStatus() error = %v", err)
		}
		if err := st.UpdateConfigApplyBatchTargetStatus(ctx, "batch-1", "agent-2", storage.ConfigApplyTargetStatusSkipped, ""); err != nil {
			t.Fatalf("UpdateConfigApplyBatchTargetStatus() error = %v", err)
		}
		_, gotTargets, err = st.GetConfigApplyBatch(ctx, "batch-1")
		if err != nil {
			t.Fatalf("GetConfigApplyBatch() after status update error = %v", err)
		}
		agent1 = targetByAgent(t, gotTargets, "agent-1")
		if agent1.Status != storage.ConfigApplyTargetStatusFailed || agent1.JobID != "job-1" {
			t.Fatalf("agent-1 target after status update = %+v, want Status=failed JobID=job-1 (unchanged)", agent1)
		}
		if agent1.Message != "health check failed" {
			t.Fatalf("agent-1 target Message = %q, want %q (must persist and survive job eviction)", agent1.Message, "health check failed")
		}
		agent2 = targetByAgent(t, gotTargets, "agent-2")
		if agent2.Status != storage.ConfigApplyTargetStatusSkipped {
			t.Fatalf("agent-2 target after status update = %+v, want Status=skipped", agent2)
		}
		if agent2.Message != "" {
			t.Fatalf("agent-2 target Message = %q, want empty", agent2.Message)
		}

		// UpdateConfigApplyBatchStatus: batch transitions to succeeded.
		succeededAt := base.Add(time.Hour)
		if err := st.UpdateConfigApplyBatchStatus(ctx, "batch-1", storage.ConfigApplyBatchStatusSucceeded, succeededAt); err != nil {
			t.Fatalf("UpdateConfigApplyBatchStatus() error = %v", err)
		}
		gotBatch, _, err = st.GetConfigApplyBatch(ctx, "batch-1")
		if err != nil {
			t.Fatalf("GetConfigApplyBatch() after batch status update error = %v", err)
		}
		if gotBatch.Status != storage.ConfigApplyBatchStatusSucceeded {
			t.Fatalf("batch-1 Status = %q, want succeeded", gotBatch.Status)
		}
		if !gotBatch.UpdatedAt.Equal(succeededAt) {
			t.Fatalf("batch-1 UpdatedAt = %v, want %v", gotBatch.UpdatedAt, succeededAt)
		}

		// A second, still-running batch in a different fleet group.
		batch2 := storage.ConfigApplyBatchRecord{
			ID:               "batch-2",
			FleetGroupID:     testFleetGroupIDB,
			Mode:             storage.ConfigApplyBatchModeRolling,
			WaveSize:         2,
			ExpectedRevision: "rev-2",
			Status:           storage.ConfigApplyBatchStatusRunning,
			CreatedAt:        base.Add(time.Minute),
			UpdatedAt:        base.Add(time.Minute),
		}
		if err := st.CreateConfigApplyBatch(ctx, batch2, []storage.ConfigApplyBatchTargetRecord{
			{BatchID: "batch-2", AgentID: "agent-3", WaveIndex: 0, Status: storage.ConfigApplyTargetStatusPending},
		}); err != nil {
			t.Fatalf("CreateConfigApplyBatch(batch-2) error = %v", err)
		}

		// A third, terminal (halted) batch — old enough to be pruned later —
		// exercises PruneConfigApplyBatches alongside batch-1 (succeeded).
		batch3 := storage.ConfigApplyBatchRecord{
			ID:               "batch-3",
			FleetGroupID:     testFleetGroupID,
			Mode:             storage.ConfigApplyBatchModeAllAtOnce,
			WaveSize:         1,
			ExpectedRevision: "rev-0",
			Status:           storage.ConfigApplyBatchStatusHalted,
			CreatedAt:        base.Add(-2 * time.Hour),
			UpdatedAt:        base.Add(-2 * time.Hour),
		}
		if err := st.CreateConfigApplyBatch(ctx, batch3, []storage.ConfigApplyBatchTargetRecord{
			{BatchID: "batch-3", AgentID: "agent-4", WaveIndex: 0, Status: storage.ConfigApplyTargetStatusFailed},
		}); err != nil {
			t.Fatalf("CreateConfigApplyBatch(batch-3) error = %v", err)
		}

		// ListRunningConfigApplyBatches: only batch-2 is running (batch-1
		// succeeded, batch-3 is halted).
		running, err := st.ListRunningConfigApplyBatches(ctx)
		if err != nil {
			t.Fatalf("ListRunningConfigApplyBatches() error = %v", err)
		}
		if len(running) != 1 || running[0].ID != "batch-2" {
			t.Fatalf("ListRunningConfigApplyBatches() = %+v, want exactly [batch-2]", running)
		}

		// ActiveConfigApplyBatchForGroup: group B has the running batch-2;
		// group A has none running (batch-1 succeeded, batch-3 halted).
		active, ok, err := st.ActiveConfigApplyBatchForGroup(ctx, testFleetGroupIDB)
		if err != nil {
			t.Fatalf("ActiveConfigApplyBatchForGroup(groupB) error = %v", err)
		}
		if !ok || active.ID != "batch-2" {
			t.Fatalf("ActiveConfigApplyBatchForGroup(groupB) = (%+v, %v), want (batch-2, true)", active, ok)
		}
		noneActive, ok, err := st.ActiveConfigApplyBatchForGroup(ctx, testFleetGroupID)
		if err != nil {
			t.Fatalf("ActiveConfigApplyBatchForGroup(groupA) error = %v", err)
		}
		if ok {
			t.Fatalf("ActiveConfigApplyBatchForGroup(groupA) = (%+v, true), want (_, false)", noneActive)
		}
		if noneActive.ID != "" {
			t.Fatalf("ActiveConfigApplyBatchForGroup(groupA) zero-value ID = %q, want empty", noneActive.ID)
		}

		// PruneConfigApplyBatches: cutoff strictly between batch-3's
		// updated_at (2h before base) and batch-1's (1h after base) prunes
		// only batch-3; batch-1 (terminal but newer than cutoff) and
		// batch-2 (running, never eligible) survive.
		cutoff := base.Add(-time.Hour)
		pruned, err := st.PruneConfigApplyBatches(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneConfigApplyBatches() error = %v", err)
		}
		if pruned != 1 {
			t.Fatalf("PruneConfigApplyBatches() pruned = %d, want 1", pruned)
		}
		if _, _, err := st.GetConfigApplyBatch(ctx, "batch-3"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetConfigApplyBatch(batch-3) after prune: want ErrNotFound, got %v", err)
		}
		if _, _, err := st.GetConfigApplyBatch(ctx, "batch-1"); err != nil {
			t.Fatalf("GetConfigApplyBatch(batch-1) after prune: want survival, got %v", err)
		}
		if _, _, err := st.GetConfigApplyBatch(ctx, "batch-2"); err != nil {
			t.Fatalf("GetConfigApplyBatch(batch-2) after prune: want survival, got %v", err)
		}

		// A wider cutoff now also prunes batch-1 (succeeded, 1h after base)
		// and cascades its targets; batch-2 stays untouched because it is
		// still running.
		pruned, err = st.PruneConfigApplyBatches(ctx, base.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("PruneConfigApplyBatches() (wide cutoff) error = %v", err)
		}
		if pruned != 1 {
			t.Fatalf("PruneConfigApplyBatches() (wide cutoff) pruned = %d, want 1", pruned)
		}
		if _, _, err := st.GetConfigApplyBatch(ctx, "batch-1"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetConfigApplyBatch(batch-1) after wide prune: want ErrNotFound, got %v", err)
		}
		if _, _, err := st.GetConfigApplyBatch(ctx, "batch-2"); err != nil {
			t.Fatalf("GetConfigApplyBatch(batch-2) after wide prune: want survival (running), got %v", err)
		}
	})

	t.Run("GetConfigApplyBatch not found", func(t *testing.T) {
		st := open(t)
		defer st.Close()

		if _, _, err := st.GetConfigApplyBatch(context.Background(), "does-not-exist"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetConfigApplyBatch(missing) error = %v, want ErrNotFound", err)
		}
	})
}

// assertBatchEqual compares every field of a ConfigApplyBatchRecord,
// using time.Time.Equal for the timestamp fields so a semantically-equal
// value with a different monotonic reading or location representation
// still passes (mirrors how the rest of this package avoids raw struct
// equality on records containing time.Time).
func assertBatchEqual(t *testing.T, got, want storage.ConfigApplyBatchRecord) {
	t.Helper()
	if got.ID != want.ID || got.FleetGroupID != want.FleetGroupID || got.Mode != want.Mode ||
		got.WaveSize != want.WaveSize || got.ExpectedRevision != want.ExpectedRevision || got.Status != want.Status {
		t.Fatalf("GetConfigApplyBatch() batch = %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("GetConfigApplyBatch() batch timestamps = (created=%v updated=%v), want (created=%v updated=%v)",
			got.CreatedAt, got.UpdatedAt, want.CreatedAt, want.UpdatedAt)
	}
}

// mustPutFleetGroup is a small helper shared by the batch contract's two
// fixture groups; kept local to this file since no other contract test
// needs a two-group setup with this exact signature.
func mustPutFleetGroup(t *testing.T, ctx context.Context, st storage.MigrationStore, id, name string, createdAt time.Time) {
	t.Helper()
	if err := st.PutFleetGroup(ctx, storage.FleetGroupRecord{
		ID:        id,
		Name:      name,
		CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf(errPutFleetGroupLong, err)
	}
}

// targetByAgent finds the target row for agentID or fails the test — every
// call site expects the row to exist.
func targetByAgent(t *testing.T, targets []storage.ConfigApplyBatchTargetRecord, agentID string) storage.ConfigApplyBatchTargetRecord {
	t.Helper()
	for _, tg := range targets {
		if tg.AgentID == agentID {
			return tg
		}
	}
	t.Fatalf("no target for agent %q in %+v", agentID, targets)
	return storage.ConfigApplyBatchTargetRecord{}
}
