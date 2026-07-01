package server

import (
	"context"
	"encoding/json"
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

// TestApplyConfigAgentSuccessRoundTrip drives the full HTTP path: it seeds an
// in-scope agent + a group target, fires the POST in a goroutine (which blocks
// on the config.apply job), then simulates the agent reporting success via
// RecordResult. The response must report applied: 1.
func TestApplyConfigAgentSuccessRoundTrip(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-success-group", time.Time{})
	const agentID = "agent-apply-ok"
	srv.seedLiveAgent(Agent{ID: agentID, FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	type result struct {
		code int
		body []byte
	}
	done := make(chan result, 1)
	go func() {
		resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/"+agentID+"/config/apply", nil, cookies)
		done <- result{code: resp.Code, body: resp.Body.Bytes()}
	}()

	// Poll for the enqueued job, then record a success for its target.
	jobID := waitForAgentJob(t, srv, agentID, jobs.ActionConfigApply)
	if !srv.jobs.RecordResult(context.Background(), agentID, jobID, true, "ok", "", time.Now()) {
		t.Fatalf("RecordResult(success) returned false")
	}

	select {
	case got := <-done:
		if got.code != http.StatusOK {
			t.Fatalf("apply status = %d, want %d (body: %s)", got.code, http.StatusOK, string(got.body))
		}
		var resp configApplyResponse
		if err := json.Unmarshal(got.body, &resp); err != nil {
			t.Fatalf("decode apply response: %v", err)
		}
		if resp.Applied != 1 {
			t.Fatalf("applied = %d, want 1 (body: %s)", resp.Applied, string(got.body))
		}
		if resp.Failed != "" || resp.Error != "" {
			t.Fatalf("failed/error = %q/%q, want empty", resp.Failed, resp.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("apply handler did not return after RecordResult")
	}
}

// TestApplyConfigAgentEmptyConfigNoOp: an in-scope agent with no group/agent
// config target resolves an empty effective config, so the apply is a no-op
// that completes immediately with applied: 1 (and no job enqueued).
func TestApplyConfigAgentEmptyConfigNoOp(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-empty-group", time.Time{})
	const agentID = "agent-apply-empty"
	srv.seedLiveAgent(Agent{ID: agentID, FleetGroupID: groupID})

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/"+agentID+"/config/apply", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("empty-config apply status = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	var got configApplyResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode apply response: %v", err)
	}
	if got.Applied != 1 || got.Failed != "" || got.Error != "" {
		t.Fatalf("empty-config apply response = %+v, want {Applied:1}", got)
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
	if len(got.Jobs) != 2 {
		t.Fatalf("jobs len = %d, want 2 (body: %s)", len(got.Jobs), resp.Body.String())
	}
	for _, h := range got.Jobs {
		if h.JobID == "" {
			t.Fatalf("job handle for %s has empty job id (body: %s)", h.AgentID, resp.Body.String())
		}
	}
}

// TestGroupConfigApplyStatusPartialFailure drives the status endpoint through a
// mixed outcome: two in-scope agents, one records success and one records
// failure. The aggregate must report Done=true, Applied=1, Failed=1, and carry
// the failing agent's ResultText — surfacing the PARTIAL rollout rather than
// masking it behind a single 5xx.
func TestGroupConfigApplyStatusPartialFailure(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-status-group", time.Time{})
	const okAgent = "agent-status-ok"
	const failAgent = "agent-status-fail"
	srv.seedLiveAgent(Agent{ID: okAgent, FleetGroupID: groupID})
	srv.seedLiveAgent(Agent{ID: failAgent, FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	// Kick off the async apply, capture the job handles.
	resp := performJSONRequest(t, srv, http.MethodPost, "/api/fleet-groups/"+groupID+"/config/apply", nil, cookies)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("group apply status = %d, want %d", resp.Code, http.StatusAccepted)
	}
	var accepted groupApplyAcceptedResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode accepted response: %v", err)
	}
	jobByAgent := map[string]string{}
	for _, h := range accepted.Jobs {
		jobByAgent[h.AgentID] = h.JobID
	}

	// One agent succeeds, one fails.
	if !srv.jobs.RecordResult(context.Background(), okAgent, jobByAgent[okAgent], true, "ok", "", time.Now()) {
		t.Fatalf("RecordResult(success) returned false")
	}
	if !srv.jobs.RecordResult(context.Background(), failAgent, jobByAgent[failAgent], false, "health check failed", "", time.Now()) {
		t.Fatalf("RecordResult(failure) returned false")
	}

	// Poll the status endpoint with the returned agent/job ids.
	statusURL := "/api/fleet-groups/" + groupID + "/config/apply/status" +
		"?agent=" + okAgent + "&job=" + jobByAgent[okAgent] +
		"&agent=" + failAgent + "&job=" + jobByAgent[failAgent]
	statusResp := performJSONRequest(t, srv, http.MethodGet, statusURL, nil, cookies)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status endpoint code = %d, want %d (body: %s)", statusResp.Code, http.StatusOK, statusResp.Body.String())
	}
	var status groupApplyStatusResponse
	if err := json.Unmarshal(statusResp.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if !status.Done {
		t.Fatalf("done = false, want true (body: %s)", statusResp.Body.String())
	}
	if status.Total != 2 || status.Applied != 1 || status.Failed != 1 || status.Pending != 0 {
		t.Fatalf("aggregate = %+v, want Total:2 Applied:1 Failed:1 Pending:0", status)
	}
	var sawFailMessage bool
	for _, a := range status.Agents {
		if a.AgentID == failAgent {
			if a.Status != applyStatusFailed {
				t.Fatalf("fail agent status = %q, want %q", a.Status, applyStatusFailed)
			}
			if a.Message != "health check failed" {
				t.Fatalf("fail agent message = %q, want %q", a.Message, "health check failed")
			}
			sawFailMessage = true
		}
	}
	if !sawFailMessage {
		t.Fatalf("failing agent not present in status agents (body: %s)", statusResp.Body.String())
	}
}

// TestGroupConfigApplyStatusPendingNotDone: a target with no reported result
// yet aggregates to Done=false and Pending>0, so the poller keeps polling.
func TestGroupConfigApplyStatusPendingNotDone(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "apply-status-pending", time.Time{})
	const agentID = "agent-status-pending"
	srv.seedLiveAgent(Agent{ID: agentID, FleetGroupID: groupID})
	seedGroupConfigTarget(t, srv, groupID, map[string]any{
		"censorship": map[string]any{"tls_domain": "example.com"},
	})

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/fleet-groups/"+groupID+"/config/apply", nil, cookies)
	var accepted groupApplyAcceptedResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode accepted response: %v", err)
	}
	if len(accepted.Jobs) != 1 {
		t.Fatalf("jobs len = %d, want 1", len(accepted.Jobs))
	}
	h := accepted.Jobs[0]
	statusURL := "/api/fleet-groups/" + groupID + "/config/apply/status?agent=" + h.AgentID + "&job=" + h.JobID
	statusResp := performJSONRequest(t, srv, http.MethodGet, statusURL, nil, cookies)
	var status groupApplyStatusResponse
	if err := json.Unmarshal(statusResp.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if status.Done {
		t.Fatalf("done = true, want false while target is unreported (body: %s)", statusResp.Body.String())
	}
	if status.Pending != 1 || status.Total != 1 {
		t.Fatalf("aggregate = %+v, want Total:1 Pending:1", status)
	}
}

// TestGroupConfigApplyStatusOutOfScope: a fleet-scoped operator hitting the
// status endpoint for an out-of-scope group gets the same 404 the sibling
// endpoints return.
func TestGroupConfigApplyStatusOutOfScope(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	inScope := seedTestFleetGroup(t, srv.store, "status-scope-in", time.Time{})
	outOfScope := seedTestFleetGroup(t, srv.store, "status-scope-out", time.Time{})
	cookies := loginScopedOperator(t, srv, "status-scoped-op", []string{inScope})

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+outOfScope+"/config/apply/status", nil, cookies)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("out-of-scope status code = %d, want %d (body: %s)", resp.Code, http.StatusNotFound, resp.Body.String())
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
