package server

import (
	"context"
	"testing"
	"time"

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
	batchID, err := srv.createGroupApplyBatch(ctx, "tester", groupID, storage.ConfigApplyBatchModeAllAtOnce, 1, []string{agentA, agentB})
	if err != nil {
		t.Fatalf("createGroupApplyBatch() error = %v", err)
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
	batchID, err := srv.createGroupApplyBatch(ctx, "tester", groupID, storage.ConfigApplyBatchModeAllAtOnce, 1, []string{okAgent, failAgent})
	if err != nil {
		t.Fatalf("createGroupApplyBatch() error = %v", err)
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
	batchID, err := srv.createGroupApplyBatch(ctx, "tester", groupID, storage.ConfigApplyBatchModeAllAtOnce, 1, []string{agentID})
	if err != nil {
		t.Fatalf("createGroupApplyBatch() error = %v", err)
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
