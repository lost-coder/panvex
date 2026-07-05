package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// seedGroupConfigTarget stores a group-scope config target with the given
// editable sections so the apply fan-out resolves a non-empty effective config.
func seedGroupConfigTarget(t *testing.T, srv *Server, groupID string, sections map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(sections)
	if err != nil {
		t.Fatalf("marshal sections: %v", err)
	}
	if err := srv.store.UpsertAgentConfigTarget(context.Background(), storage.AgentConfigTargetRecord{
		ScopeType:    storage.ConfigScopeGroup,
		ScopeID:      groupID,
		SectionsJSON: string(encoded),
	}); err != nil {
		t.Fatalf("UpsertAgentConfigTarget: %v", err)
	}
}

// waitForAgentJob polls the job store until a job of the given action targeting
// agentID appears, returning its id. Fails the test if none shows up in time.
func waitForAgentJob(t *testing.T, srv *Server, agentID string, action jobs.Action) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, job := range srv.jobs.ListWithContext(context.Background()) {
			if job.Action != action {
				continue
			}
			for _, tgt := range job.Targets {
				if tgt.AgentID == agentID {
					return job.ID
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s job targeting %s appeared within deadline", action, agentID)
	return ""
}

// TestApplyConfigGroupOutOfScope: a fleet-scoped operator whose scope excludes
// the target group gets the same 404 the sibling endpoints return.
func TestApplyConfigGroupOutOfScope(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	inScope := seedTestFleetGroup(t, srv.store, "apply-scope-in", time.Time{})
	outOfScope := seedTestFleetGroup(t, srv.store, "apply-scope-out", time.Time{})
	cookies := loginScopedOperator(t, srv, "apply-scoped-op-group", []string{inScope})

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/fleet-groups/"+outOfScope+"/config/apply", nil, cookies)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("out-of-scope group apply status = %d, want %d (body: %s)", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

// TestApplyConfigAgentOutOfScope: a fleet-scoped operator applying an agent
// whose fleet group is out of scope gets the agent not-found response.
func TestApplyConfigAgentOutOfScope(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	inScope := seedTestFleetGroup(t, srv.store, "apply-agent-scope-in", time.Time{})
	outOfScope := seedTestFleetGroup(t, srv.store, "apply-agent-scope-out", time.Time{})
	const agentID = "agent-apply-oos"
	srv.seedLiveAgent(Agent{ID: agentID, FleetGroupID: outOfScope})
	cookies := loginScopedOperator(t, srv, "apply-scoped-op-agent", []string{inScope})

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/"+agentID+"/config/apply", nil, cookies)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("out-of-scope agent apply status = %d, want %d (body: %s)", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

// TestApplyConfigAgentUnknown: applying an agent that does not exist in the
// live snapshot returns 404.
func TestApplyConfigAgentUnknown(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/does-not-exist/config/apply", nil, cookies)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("unknown agent apply status = %d, want %d (body: %s)", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

// TestApplyConfigAgentSuccessRoundTrip (P3-3.4): the single-agent apply is now
// an async batch-of-one — the POST returns 202 + batch_id immediately (no
// in-handler poll), persisting an AGENT-SCOPED batch (empty fleet_group_id)
// with exactly one target for the agent.
func TestApplyConfigAgentSuccessRoundTrip(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-success-group", time.Time{})
	const agentID = "agent-apply-ok"
	srv.seedLiveAgent(Agent{ID: agentID, FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/"+agentID+"/config/apply", nil, cookies)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("apply status = %d, want %d (body: %s)", resp.Code, http.StatusAccepted, resp.Body.String())
	}
	var accepted groupApplyAcceptedResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &accepted); err != nil || accepted.BatchID == "" {
		t.Fatalf("body = %s, want batch_id", resp.Body.String())
	}

	batch, targets, err := srv.store.GetConfigApplyBatch(context.Background(), accepted.BatchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch() error = %v", err)
	}
	if batch.FleetGroupID != "" {
		t.Errorf("FleetGroupID = %q, want empty (agent-scoped)", batch.FleetGroupID)
	}
	if len(targets) != 1 || targets[0].AgentID != agentID {
		t.Fatalf("targets = %+v, want single %s", targets, agentID)
	}
	if targets[0].JobID == "" {
		t.Errorf("target job_id empty, want the enqueued config.apply job id")
	}

	// The agent-scoped batch-status endpoint resolves through the agent's URL.
	statusResp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/"+agentID+"/config/apply/batches/"+accepted.BatchID, nil, cookies)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("batch status = %d, want 200 (body: %s)", statusResp.Code, statusResp.Body.String())
	}
}

// TestApplyConfigAgentEmptyConfigNoOp: an in-scope agent with no group/agent
// config target resolves an empty effective config, so the batch's single
// target is immediately succeeded (no job enqueued) — the POST still returns
// 202 + batch_id (P3-3.4).
func TestApplyConfigAgentEmptyConfigNoOp(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-empty-group", time.Time{})
	const agentID = "agent-apply-empty"
	srv.seedLiveAgent(Agent{ID: agentID, FleetGroupID: groupID})

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/"+agentID+"/config/apply", nil, cookies)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("empty-config apply status = %d, want %d (body: %s)", resp.Code, http.StatusAccepted, resp.Body.String())
	}
	var accepted groupApplyAcceptedResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &accepted); err != nil || accepted.BatchID == "" {
		t.Fatalf("body = %s, want batch_id", resp.Body.String())
	}
	_, targets, err := srv.store.GetConfigApplyBatch(context.Background(), accepted.BatchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch() error = %v", err)
	}
	if len(targets) != 1 || targets[0].Status != storage.ConfigApplyTargetStatusSucceeded {
		t.Fatalf("target = %+v, want single succeeded (empty effective config)", targets)
	}
}

// TestApplyConfigGroupAsyncAccepted asserts the group apply is ASYNC: the POST
// returns 202 Accepted immediately (WITHOUT waiting on any config.apply job
// reaching terminal status) and the body carries a batch id plus one job
// handle per in-scope agent. Pre-change the handler blocked on
// waitJobTargetTerminal and returned 200 only after each target completed —
// with no agent reporting a result, this test would hang/time out.
func TestApplyConfigGroupAsyncAccepted(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-async-group", time.Time{})
	srv.seedLiveAgent(Agent{ID: "agent-async-1", FleetGroupID: groupID})
	srv.seedLiveAgent(Agent{ID: "agent-async-2", FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	// No goroutine, no RecordResult — a synchronous handler would block here.
	resp := performJSONRequest(t, srv, http.MethodPost, "/api/fleet-groups/"+groupID+"/config/apply", nil, cookies)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("group apply status = %d, want %d (body: %s)", resp.Code, http.StatusAccepted, resp.Body.String())
	}
	var got groupApplyAcceptedResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode accepted response: %v", err)
	}
	if got.BatchID == "" {
		t.Fatalf("batch_id empty, want non-empty (body: %s)", resp.Body.String())
	}
	// P3-3.4: per-job handles were removed from the 202 body; the persisted
	// batch is the source of truth (asserted by TestApplyConfigGroupPersistsBatch).
	batch, targets, err := srv.store.GetConfigApplyBatch(context.Background(), got.BatchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s): %v", got.BatchID, err)
	}
	if batch.FleetGroupID != groupID || len(targets) != 2 {
		t.Fatalf("batch = %+v targets = %+v, want group-scoped with 2 targets", batch, targets)
	}
}

// TestApplyConfigGroupPersistsBatch asserts the group-apply POST persists a
// config_apply_batches row (Phase A2): the response batch_id must resolve via
// srv.store.GetConfigApplyBatch to a running all-at-once batch with one
// wave-0 target per in-scope agent, each target running with a non-empty
// job_id. The handler must return promptly — no waitJobTargetTerminal — so
// this asserts on wall-clock elapsed time with no RecordResult in sight.
func TestApplyConfigGroupPersistsBatch(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-persist-group", time.Time{})
	srv.seedLiveAgent(Agent{ID: "agent-persist-1", FleetGroupID: groupID})
	srv.seedLiveAgent(Agent{ID: "agent-persist-2", FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	start := time.Now()
	resp := performJSONRequest(t, srv, http.MethodPost, "/api/fleet-groups/"+groupID+"/config/apply", nil, cookies)
	elapsed := time.Since(start)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("group apply status = %d, want %d (body: %s)", resp.Code, http.StatusAccepted, resp.Body.String())
	}
	if elapsed > 2*time.Second {
		t.Fatalf("group apply took %v, want prompt return (no terminal wait)", elapsed)
	}
	var got groupApplyAcceptedResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode accepted response: %v", err)
	}
	if got.BatchID == "" {
		t.Fatalf("batch_id empty, want non-empty (body: %s)", resp.Body.String())
	}

	batch, targets, err := srv.store.GetConfigApplyBatch(context.Background(), got.BatchID)
	if err != nil {
		t.Fatalf("GetConfigApplyBatch(%s): %v", got.BatchID, err)
	}
	if batch.Status != storage.ConfigApplyBatchStatusRunning {
		t.Fatalf("batch status = %q, want %q", batch.Status, storage.ConfigApplyBatchStatusRunning)
	}
	if batch.FleetGroupID != groupID {
		t.Fatalf("batch fleet_group_id = %q, want %q", batch.FleetGroupID, groupID)
	}
	if batch.Mode != storage.ConfigApplyBatchModeAllAtOnce {
		t.Fatalf("batch mode = %q, want %q", batch.Mode, storage.ConfigApplyBatchModeAllAtOnce)
	}
	if len(targets) != 2 {
		t.Fatalf("targets len = %d, want 2 (targets: %+v)", len(targets), targets)
	}
	seen := map[string]bool{}
	for _, tgt := range targets {
		if tgt.WaveIndex != 0 {
			t.Fatalf("target %s wave_index = %d, want 0", tgt.AgentID, tgt.WaveIndex)
		}
		if tgt.Status != storage.ConfigApplyTargetStatusRunning {
			t.Fatalf("target %s status = %q, want %q", tgt.AgentID, tgt.Status, storage.ConfigApplyTargetStatusRunning)
		}
		if tgt.JobID == "" {
			t.Fatalf("target %s job_id empty, want non-empty", tgt.AgentID)
		}
		seen[tgt.AgentID] = true
	}
	if !seen["agent-persist-1"] || !seen["agent-persist-2"] {
		t.Fatalf("targets missing expected agents: %+v", targets)
	}
}

// TestWaitJobTargetTerminalSucceeded enqueues a config.apply job, records a
// success, and asserts waitJobTargetTerminal returns nil.
func TestWaitJobTargetTerminalSucceeded(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	const agentID = "wait-ok-agent"
	job, err := srv.jobs.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionConfigApply,
		TargetAgentIDs: []string{agentID},
		TTL:            configApplyJobTTL,
		ActorID:        "tester",
		PayloadJSON:    `{"patch":{},"health_timeout_s":30}`,
	}, time.Now())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !srv.jobs.RecordResult(context.Background(), agentID, job.ID, true, "applied", "", time.Now()) {
		t.Fatalf("RecordResult(success) returned false")
	}
	if err := srv.waitJobTargetTerminal(context.Background(), job.ID, agentID, "config.apply"); err != nil {
		t.Fatalf("waitJobTargetTerminal after success = %v, want nil", err)
	}
}

// TestWaitJobTargetTerminalFailed enqueues a config.apply job, records a
// failure, and asserts waitJobTargetTerminal returns a non-nil error.
func TestWaitJobTargetTerminalFailed(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	const agentID = "wait-fail-agent"
	job, err := srv.jobs.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionConfigApply,
		TargetAgentIDs: []string{agentID},
		TTL:            configApplyJobTTL,
		ActorID:        "tester",
		PayloadJSON:    `{"patch":{},"health_timeout_s":30}`,
	}, time.Now())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if !srv.jobs.RecordResult(context.Background(), agentID, job.ID, false, "health check failed", "", time.Now()) {
		t.Fatalf("RecordResult(failure) returned false")
	}
	if err := srv.waitJobTargetTerminal(context.Background(), job.ID, agentID, "config.apply"); err == nil {
		t.Fatalf("waitJobTargetTerminal after failure = nil, want error")
	}
}

// failOnAgentStore wraps a storage.Store and returns injectedErr from
// SetConfigApplyBatchTargetJob the first time it is called for failAgentID,
// leaving every other call (and every other method) to pass through to the
// wrapped store unchanged. Used to force a deterministic mid-loop failure in
// createConfigApplyBatch's per-agent fan-out.
type failOnAgentStore struct {
	storage.Store
	failAgentID string
	injectedErr error
}

func (f *failOnAgentStore) SetConfigApplyBatchTargetJob(ctx context.Context, batchID, agentID, jobID, status string) error {
	if agentID == f.failAgentID {
		return f.injectedErr
	}
	return f.Store.SetConfigApplyBatchTargetJob(ctx, batchID, agentID, jobID, status)
}

// TestCreateGroupApplyBatchMidLoopFailureMarksBatchFailed is the regression
// test for the review finding: if a per-agent SetConfigApplyBatchTargetJob
// call fails partway through the fan-out loop, the batch row must NOT be
// left stuck in "running" forever (pre-fix behavior — nothing ever
// terminates it, so the operator/UI sees what looks like a permanently
// in-progress rollout). Post-fix, createConfigApplyBatch marks the batch
// ConfigApplyBatchStatusFailed before returning the error.
func TestCreateGroupApplyBatchMidLoopFailureMarksBatchFailed(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-midloop-fail-group", time.Time{})
	const okAgent = "agent-midloop-ok"
	const failAgent = "agent-midloop-fail"
	srv.seedLiveAgent(Agent{ID: okAgent, FleetGroupID: groupID})
	srv.seedLiveAgent(Agent{ID: failAgent, FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	injectedErr := fmt.Errorf("injected mid-loop failure for %s", failAgent)
	realStore := srv.store
	srv.store = &failOnAgentStore{Store: realStore, failAgentID: failAgent, injectedErr: injectedErr}

	// Agent order is exactly what we pass in, so okAgent is enqueued/recorded
	// first (succeeds) and failAgent second (fails), reproducing the
	// mid-loop scenario deterministically rather than depending on map
	// iteration order.
	batchID, err := srv.createConfigApplyBatch(context.Background(), "tester", groupID, []string{okAgent, failAgent})
	if err == nil {
		t.Fatalf("createConfigApplyBatch() error = nil, want non-nil (injected failure for %s)", failAgent)
	}
	if batchID == "" {
		t.Fatalf("createConfigApplyBatch() batchID = %q, want non-empty even on mid-loop failure", batchID)
	}

	// Restore the real store to read the batch back without the injected
	// failure interfering.
	srv.store = realStore
	batch, _, getErr := srv.store.GetConfigApplyBatch(context.Background(), batchID)
	if getErr != nil {
		t.Fatalf("GetConfigApplyBatch(%s): %v", batchID, getErr)
	}
	if batch.Status != storage.ConfigApplyBatchStatusFailed {
		t.Fatalf("batch status = %q, want %q (batch must not be left running after a mid-loop failure)", batch.Status, storage.ConfigApplyBatchStatusFailed)
	}
}

// TestCreateGroupApplyBatchEmptyAgentListNoBatch asserts the documented
// empty-scope contract: calling createConfigApplyBatch with no agent ids
// returns ("", nil) — the same as a group with no in-scope agents — and
// creates no batch row at all (there is nothing to enqueue and nothing for
// an operator to inspect).
func TestCreateGroupApplyBatchEmptyAgentListNoBatch(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-empty-agents-group", time.Time{})

	batchID, err := srv.createConfigApplyBatch(context.Background(), "tester", groupID, nil)
	if err != nil {
		t.Fatalf("createConfigApplyBatch(empty agentIDs) error = %v, want nil", err)
	}
	if batchID != "" {
		t.Fatalf("createConfigApplyBatch(empty agentIDs) batchID = %q, want empty", batchID)
	}

	running, listErr := srv.store.ListRunningConfigApplyBatches(context.Background())
	if listErr != nil {
		t.Fatalf("ListRunningConfigApplyBatches: %v", listErr)
	}
	for _, b := range running {
		if b.FleetGroupID == groupID {
			t.Fatalf("found a batch row for group %s after an empty-agent-list call, want none: %+v", groupID, b)
		}
	}
}

// TestApplyConfigGroupEmptyScopeNoBatchViaHTTP drives the same empty-scope
// contract through the HTTP handler: a group with zero in-scope (live)
// agents must still return 202 Accepted with an empty job list, and must not
// create a batch row.
func TestApplyConfigGroupEmptyScopeNoBatchViaHTTP(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-empty-scope-http-group", time.Time{})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/fleet-groups/"+groupID+"/config/apply", nil, cookies)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("empty-scope group apply status = %d, want %d (body: %s)", resp.Code, http.StatusAccepted, resp.Body.String())
	}
	var got groupApplyAcceptedResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode accepted response: %v", err)
	}
	// P3-3.4: no in-scope agents ⇒ no batch created ⇒ empty batch_id.
	if got.BatchID != "" {
		t.Fatalf("batch_id = %q, want empty for a group with no in-scope agents (body: %s)", got.BatchID, resp.Body.String())
	}

	running, listErr := srv.store.ListRunningConfigApplyBatches(context.Background())
	if listErr != nil {
		t.Fatalf("ListRunningConfigApplyBatches: %v", listErr)
	}
	for _, b := range running {
		if b.FleetGroupID == groupID {
			t.Fatalf("found a batch row for empty-scope group %s, want none: %+v", groupID, b)
		}
	}
}

// TestWaitJobTargetTerminalCtxCancel asserts a cancelled context aborts the
// wait with the context error rather than blocking until the deadline.
func TestWaitJobTargetTerminalCtxCancel(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	const agentID = "wait-cancel-agent"
	job, err := srv.jobs.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionConfigApply,
		TargetAgentIDs: []string{agentID},
		TTL:            configApplyJobTTL,
		ActorID:        "tester",
		PayloadJSON:    `{"patch":{},"health_timeout_s":30}`,
	}, time.Now())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := srv.waitJobTargetTerminal(ctx, job.ID, agentID, "config.apply"); err == nil {
		t.Fatalf("waitJobTargetTerminal with cancelled ctx = nil, want error")
	}
}

// TestAgentApplyBatchStatusForeignBatchNotFound (P3-3.4): a batch belonging to
// one agent, fetched through a different agent's URL, returns 404 — membership
// is checked by the batch's target (agent-scoped batches have no group), so a
// foreign batch's existence is never revealed.
func TestAgentApplyBatchStatusForeignBatchNotFound(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-foreign-group", time.Time{})
	srv.seedLiveAgent(Agent{ID: "agent-a", FleetGroupID: groupID})
	srv.seedLiveAgent(Agent{ID: "agent-b", FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/agent-a/config/apply", nil, cookies)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("apply status = %d, want 202 (body: %s)", resp.Code, resp.Body.String())
	}
	var accepted groupApplyAcceptedResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &accepted); err != nil || accepted.BatchID == "" {
		t.Fatalf("body = %s, want batch_id", resp.Body.String())
	}

	foreignResp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/agent-b/config/apply/batches/"+accepted.BatchID, nil, cookies)
	if foreignResp.Code != http.StatusNotFound {
		t.Fatalf("foreign agent status = %d, want 404", foreignResp.Code)
	}

	ownResp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/agent-a/config/apply/batches/"+accepted.BatchID, nil, cookies)
	if ownResp.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200", ownResp.Code)
	}
	var status struct {
		Total int  `json:"total"`
		Done  bool `json:"done"`
	}
	if err := json.Unmarshal(ownResp.Body.Bytes(), &status); err != nil || status.Total != 1 {
		t.Fatalf("owner body = %s, want total=1", ownResp.Body.String())
	}
}
