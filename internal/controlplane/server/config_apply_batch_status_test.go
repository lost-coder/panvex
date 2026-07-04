package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// TestGetGroupApplyBatchStatusResumableWithoutLiveJob is the headline
// resumability test (A4 requirement a): a batch is seeded directly via the
// store with a target already terminal-failed and its message persisted on
// the target row — job_id points at a job that was never enqueued in this
// process (simulating one evicted from the in-memory jobs store after a
// panel restart). The status-by-id endpoint must still surface the failure
// message, proving the aggregate is built from the persisted row rather than
// re-derived from job state for already-terminal targets.
func TestGetGroupApplyBatchStatusResumableWithoutLiveJob(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "batch-status-resumable-group", time.Time{})

	const okAgent = "batch-status-ok-agent"
	const failAgent = "batch-status-fail-agent"
	batch := storage.ConfigApplyBatchRecord{
		ID:           "batch-resumable-1",
		FleetGroupID: groupID,
		Mode:         storage.ConfigApplyBatchModeAllAtOnce,
		WaveSize:     1,
		Status:       storage.ConfigApplyBatchStatusFailed,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	targets := []storage.ConfigApplyBatchTargetRecord{
		{BatchID: batch.ID, AgentID: okAgent, WaveIndex: 0, JobID: "job-evicted-ok", Status: storage.ConfigApplyTargetStatusSucceeded},
		{BatchID: batch.ID, AgentID: failAgent, WaveIndex: 0, JobID: "job-evicted-fail", Status: storage.ConfigApplyTargetStatusFailed, Message: "health check failed"},
	}
	if err := srv.store.CreateConfigApplyBatch(context.Background(), batch, targets); err != nil {
		t.Fatalf("CreateConfigApplyBatch() error = %v", err)
	}

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+groupID+"/config/apply/batches/"+batch.ID, nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	var got groupApplyBatchStatusResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.BatchID != batch.ID || got.Mode != storage.ConfigApplyBatchModeAllAtOnce || got.Status != storage.ConfigApplyBatchStatusFailed {
		t.Fatalf("aggregate header = %+v, want BatchID=%s Mode=%s Status=%s", got, batch.ID, storage.ConfigApplyBatchModeAllAtOnce, storage.ConfigApplyBatchStatusFailed)
	}
	if !got.Done {
		t.Fatalf("done = false, want true (batch status is terminal)")
	}
	if got.Total != 2 || got.Applied != 1 || got.Failed != 1 || got.Pending != 0 || got.Skipped != 0 {
		t.Fatalf("aggregate counts = %+v, want Total:2 Applied:1 Failed:1", got)
	}
	var sawMessage bool
	for _, a := range got.Agents {
		if a.AgentID == failAgent {
			if a.Status != storage.ConfigApplyTargetStatusFailed {
				t.Fatalf("fail agent status = %q, want %q", a.Status, storage.ConfigApplyTargetStatusFailed)
			}
			if a.Message != "health check failed" {
				t.Fatalf("fail agent message = %q, want %q (must come from persisted row, no live job exists)", a.Message, "health check failed")
			}
			sawMessage = true
		}
	}
	if !sawMessage {
		t.Fatalf("failing agent not present in response (body: %s)", resp.Body.String())
	}
}

// TestGetGroupApplyBatchStatusScopeMismatch404 asserts a batch that belongs
// to a different fleet group than the URL's {id} is reported not-found
// rather than leaking cross-group batch data (A4 requirement c).
func TestGetGroupApplyBatchStatusScopeMismatch404(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupA := seedTestFleetGroup(t, srv.store, "batch-scope-mismatch-a", time.Time{})
	groupB := seedTestFleetGroup(t, srv.store, "batch-scope-mismatch-b", time.Time{})

	batch := storage.ConfigApplyBatchRecord{
		ID:           "batch-scope-mismatch-1",
		FleetGroupID: groupA,
		Mode:         storage.ConfigApplyBatchModeAllAtOnce,
		WaveSize:     1,
		Status:       storage.ConfigApplyBatchStatusRunning,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := srv.store.CreateConfigApplyBatch(context.Background(), batch, []storage.ConfigApplyBatchTargetRecord{
		{BatchID: batch.ID, AgentID: "agent-scope-mismatch", WaveIndex: 0, Status: storage.ConfigApplyTargetStatusPending},
	}); err != nil {
		t.Fatalf("CreateConfigApplyBatch() error = %v", err)
	}

	// Requesting the batch under the wrong group id must 404, even though
	// the operator can see group B and the batch itself genuinely exists.
	resp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+groupB+"/config/apply/batches/"+batch.ID, nil, cookies)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("cross-group batch status code = %d, want %d (body: %s)", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

// TestGetGroupApplyBatchStatusOutOfFleetScope404 asserts a fleet-scoped
// operator whose scope excludes the batch's group gets the same 404 the
// sibling config-apply endpoints return (A4 requirement c, operator-scope
// variant).
func TestGetGroupApplyBatchStatusOutOfFleetScope404(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	inScope := seedTestFleetGroup(t, srv.store, "batch-fleet-scope-in", time.Time{})
	outOfScope := seedTestFleetGroup(t, srv.store, "batch-fleet-scope-out", time.Time{})
	cookies := loginScopedOperator(t, srv, "batch-status-scoped-op", []string{inScope})

	batch := storage.ConfigApplyBatchRecord{
		ID:           "batch-fleet-scope-1",
		FleetGroupID: outOfScope,
		Mode:         storage.ConfigApplyBatchModeAllAtOnce,
		WaveSize:     1,
		Status:       storage.ConfigApplyBatchStatusRunning,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := srv.store.CreateConfigApplyBatch(context.Background(), batch, []storage.ConfigApplyBatchTargetRecord{
		{BatchID: batch.ID, AgentID: "agent-fleet-scope", WaveIndex: 0, Status: storage.ConfigApplyTargetStatusPending},
	}); err != nil {
		t.Fatalf("CreateConfigApplyBatch() error = %v", err)
	}

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+outOfScope+"/config/apply/batches/"+batch.ID, nil, cookies)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("out-of-scope batch status code = %d, want %d (body: %s)", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

// TestGetGroupApplyBatchStatusUnknownBatch404 asserts an id that resolves to
// no batch at all (not merely a mismatched group) also 404s.
func TestGetGroupApplyBatchStatusUnknownBatch404(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "batch-unknown-group", time.Time{})

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+groupID+"/config/apply/batches/does-not-exist", nil, cookies)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("unknown batch status code = %d, want %d (body: %s)", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

// TestGetActiveGroupApplyBatchReturnsRunningID drives the ?active=1 lookup
// (A4 requirement b): with a running batch persisted for the group, the
// endpoint returns it; once no batch is running, it returns 204 No Content.
func TestGetActiveGroupApplyBatchReturnsRunningID(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "batch-active-group", time.Time{})

	// No batch yet: active lookup must be empty (204).
	resp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+groupID+"/config/apply/batches?active=1", nil, cookies)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("active batch status (none) = %d, want %d (body: %s)", resp.Code, http.StatusNoContent, resp.Body.String())
	}

	batch := storage.ConfigApplyBatchRecord{
		ID:           "batch-active-1",
		FleetGroupID: groupID,
		Mode:         storage.ConfigApplyBatchModeAllAtOnce,
		WaveSize:     1,
		Status:       storage.ConfigApplyBatchStatusRunning,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := srv.store.CreateConfigApplyBatch(context.Background(), batch, []storage.ConfigApplyBatchTargetRecord{
		{BatchID: batch.ID, AgentID: "agent-active", WaveIndex: 0, Status: storage.ConfigApplyTargetStatusPending},
	}); err != nil {
		t.Fatalf("CreateConfigApplyBatch() error = %v", err)
	}

	resp = performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+groupID+"/config/apply/batches?active=1", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("active batch status = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	var got groupApplyActiveBatchResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.BatchID != batch.ID {
		t.Fatalf("active batch id = %q, want %q", got.BatchID, batch.ID)
	}

	// Finalize the batch: it must drop out of the active lookup again.
	if err := srv.store.UpdateConfigApplyBatchStatus(context.Background(), batch.ID, storage.ConfigApplyBatchStatusSucceeded, time.Now()); err != nil {
		t.Fatalf("UpdateConfigApplyBatchStatus() error = %v", err)
	}
	resp = performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+groupID+"/config/apply/batches?active=1", nil, cookies)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("active batch status (after finalize) = %d, want %d (body: %s)", resp.Code, http.StatusNoContent, resp.Body.String())
	}
}

// TestGetActiveGroupApplyBatchOutOfScope404 mirrors the sibling endpoints'
// scope check for the active-batch lookup.
func TestGetActiveGroupApplyBatchOutOfScope404(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	inScope := seedTestFleetGroup(t, srv.store, "batch-active-scope-in", time.Time{})
	outOfScope := seedTestFleetGroup(t, srv.store, "batch-active-scope-out", time.Time{})
	cookies := loginScopedOperator(t, srv, "batch-active-scoped-op", []string{inScope})

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+outOfScope+"/config/apply/batches?active=1", nil, cookies)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("out-of-scope active batch status = %d, want %d (body: %s)", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

// TestAdvanceConfigApplyBatchPersistsMessageForStatusEndpoint is the
// end-to-end round trip for A4 requirement d: a real failing job is driven
// through advanceConfigApplyBatch (A3's orchestrator step), and the
// status-by-id endpoint must then report the persisted failure message —
// proving the message written by advanceConfigApplyBatch is exactly what the
// resumable view reads back.
func TestAdvanceConfigApplyBatchPersistsMessageForStatusEndpoint(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "batch-advance-message-group", time.Time{})
	const failAgent = "agent-advance-message-fail"
	srv.seedLiveAgent(Agent{ID: failAgent, FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	ctx := context.Background()
	batchID, err := srv.createConfigApplyBatch(ctx, "tester", groupID, storage.ConfigApplyBatchModeAllAtOnce, 1, []string{failAgent})
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
	if !srv.jobs.RecordResult(ctx, failAgent, targets[0].JobID, false, "disk full", "", time.Now()) {
		t.Fatalf("RecordResult(failure) returned false")
	}

	if err := srv.advanceConfigApplyBatch(ctx, batch); err != nil {
		t.Fatalf("advanceConfigApplyBatch() error = %v", err)
	}

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+groupID+"/config/apply/batches/"+batchID, nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	var got groupApplyBatchStatusResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.Done || got.Status != storage.ConfigApplyBatchStatusFailed || got.Failed != 1 {
		t.Fatalf("aggregate = %+v, want Done:true Status:failed Failed:1", got)
	}
	if len(got.Agents) != 1 || got.Agents[0].Message != "disk full" {
		t.Fatalf("agents = %+v, want a single agent with message %q", got.Agents, "disk full")
	}

	// The persisted target row itself must also carry the message (not just
	// the HTTP aggregate) — this is what makes it survive job eviction.
	_, gotTargets, err := srv.store.GetConfigApplyBatch(ctx, batchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s) after advance: %v", batchID, err)
	}
	if len(gotTargets) != 1 || gotTargets[0].Message != "disk full" {
		t.Fatalf("persisted target = %+v, want Message %q", gotTargets, "disk full")
	}
}
