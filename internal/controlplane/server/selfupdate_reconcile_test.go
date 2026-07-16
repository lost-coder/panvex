package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// countSelfUpdateJobs returns how many agent.self-update jobs are queued.
func countSelfUpdateJobs(t *testing.T, srv *Server) int {
	t.Helper()
	listed := srv.jobs.ListRecentWithContext(t.Context(), 100)
	n := 0
	for i := range listed {
		if listed[i].Action == jobs.ActionAgentSelfUpdate {
			n++
		}
	}
	return n
}

// lastSelfUpdateJob returns the most-recently-listed agent.self-update job.
func lastSelfUpdateJob(t *testing.T, srv *Server) *jobs.Job {
	t.Helper()
	listed := srv.jobs.ListRecentWithContext(t.Context(), 100)
	for i := range listed {
		if listed[i].Action == jobs.ActionAgentSelfUpdate {
			return &listed[i]
		}
	}
	return nil
}

// TestHandleAgentUpdateRecordsPendingTarget: the operator's click is the only
// place the desired version is known, so the dispatch must persist it. Without
// this record the reconcile-on-reconnect has nothing to re-send and an offline
// node loses the update when the job's TTL elapses.
func TestHandleAgentUpdateRecordsPendingTarget(t *testing.T) {
	srv, cookies := setupTransportModeServer(t)

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/agent-tm-1/update",
		map[string]string{"version": "1.4.0"}, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST /agents/agent-tm-1/update status = %d, want %d (body=%s)",
			resp.Code, http.StatusOK, resp.Body.String())
	}

	version, ok, err := srv.updatesSvc.PendingAgentUpdate(t.Context(), "agent-tm-1")
	if err != nil {
		t.Fatalf("PendingAgentUpdate() error = %v", err)
	}
	if !ok || version != "1.4.0" {
		t.Fatalf("pending target after dispatch = %q/%v, want 1.4.0/true", version, ok)
	}
}

// TestDeregisterAgentDropsPendingSelfUpdate: deregistering a node must not
// leave its desired-state entry behind. The pending map is persisted, so a
// leaked key survives forever — the blob would grow monotonically with
// tombstones of dead agents, and every reconnect of any agent unmarshals it.
func TestDeregisterAgentDropsPendingSelfUpdate(t *testing.T) {
	srv, cookies := setupTransportModeServer(t)
	ctx := t.Context()

	if err := srv.updatesSvc.SetPendingAgentUpdate(ctx, "agent-tm-1", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate() error = %v", err)
	}

	resp := performJSONRequest(t, srv, http.MethodDelete, "/api/agents/agent-tm-1", nil, cookies)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("DELETE /agents/agent-tm-1 status = %d, want %d (body=%s)",
			resp.Code, http.StatusNoContent, resp.Body.String())
	}

	if _, ok, _ := srv.updatesSvc.PendingAgentUpdate(ctx, "agent-tm-1"); ok {
		t.Fatal("a deregistered agent's pending self-update target must be dropped")
	}
	srv.mu.Lock()
	_, throttled := srv.selfUpdateReenqueuedAt["agent-tm-1"]
	srv.mu.Unlock()
	if throttled {
		t.Fatal("a deregistered agent's throttle entry must be dropped")
	}
}

// TestReconcileAgentSelfUpdateReenqueuesForStaleVersion: the operator asked for
// 1.4.0 while the node was offline, so the original job expired unseen (TTL
// 10m). On reconnect the agent still reports 1.3.0 — the pending target must be
// re-dispatched, otherwise the update is lost forever (D-1).
func TestReconcileAgentSelfUpdateReenqueuesForStaleVersion(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()

	srv.seedLiveAgentKeyed("agent-su", Agent{
		ID:       "agent-su",
		NodeName: "su-node",
		Version:  "1.3.0",
	})
	if err := srv.updatesSvc.SetPendingAgentUpdate(ctx, "agent-su", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate() error = %v", err)
	}

	srv.reconcileAgentSelfUpdate(ctx, "agent-su")

	if got := countSelfUpdateJobs(t, srv); got != 1 {
		t.Fatalf("self-update jobs after reconcile = %d, want 1", got)
	}
	job := lastSelfUpdateJob(t, srv)
	if job == nil {
		t.Fatal("no agent.self-update job enqueued")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["version"] != "1.4.0" {
		t.Fatalf("payload version = %v, want 1.4.0", payload["version"])
	}
	if job.ActorID != selfUpdateReconcileActor {
		t.Fatalf("ActorID = %q, want %q", job.ActorID, selfUpdateReconcileActor)
	}
	// The target is still outstanding: the agent has not reported 1.4.0 yet.
	if _, ok, _ := srv.updatesSvc.PendingAgentUpdate(ctx, "agent-su"); !ok {
		t.Fatal("pending target must survive until the agent reports the version")
	}
}

// TestReconcileAgentSelfUpdateThrottlesReenqueue: a flapping node that
// reconnects repeatedly inside one job TTL must not accumulate a queue of
// identical self-update jobs.
func TestReconcileAgentSelfUpdateThrottlesReenqueue(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()

	srv.seedLiveAgentKeyed("agent-su", Agent{ID: "agent-su", NodeName: "su-node", Version: "1.3.0"})
	if err := srv.updatesSvc.SetPendingAgentUpdate(ctx, "agent-su", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate() error = %v", err)
	}

	srv.reconcileAgentSelfUpdate(ctx, "agent-su")
	srv.reconcileAgentSelfUpdate(ctx, "agent-su")
	srv.reconcileAgentSelfUpdate(ctx, "agent-su")

	if got := countSelfUpdateJobs(t, srv); got != 1 {
		t.Fatalf("self-update jobs after three reconnects inside the TTL = %d, want 1", got)
	}
}

// TestReconcileAgentSelfUpdateClearsWhenVersionReached: the agent came back
// already running the requested version (the job landed before it went away, or
// the operator updated it by hand) — the desired state is satisfied, so it must
// be dropped and nothing re-dispatched.
func TestReconcileAgentSelfUpdateClearsWhenVersionReached(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()

	srv.seedLiveAgentKeyed("agent-su", Agent{ID: "agent-su", NodeName: "su-node", Version: "v1.4.0"})
	if err := srv.updatesSvc.SetPendingAgentUpdate(ctx, "agent-su", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate() error = %v", err)
	}

	srv.reconcileAgentSelfUpdate(ctx, "agent-su")

	if got := countSelfUpdateJobs(t, srv); got != 0 {
		t.Fatalf("self-update jobs when the version already matches = %d, want 0", got)
	}
	if _, ok, _ := srv.updatesSvc.PendingAgentUpdate(ctx, "agent-su"); ok {
		t.Fatal("a satisfied pending target must be cleared")
	}
}

// TestReconcileAgentSelfUpdateNoPendingIsNoOp: the overwhelmingly common case —
// an agent reconnects with nothing pending. No job, no error, no write.
func TestReconcileAgentSelfUpdateNoPendingIsNoOp(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()

	srv.seedLiveAgentKeyed("agent-su", Agent{ID: "agent-su", NodeName: "su-node", Version: "1.3.0"})

	srv.reconcileAgentSelfUpdate(ctx, "agent-su")

	if got := countSelfUpdateJobs(t, srv); got != 0 {
		t.Fatalf("self-update jobs without a pending target = %d, want 0", got)
	}
}

// TestReconcileAgentSelfUpdateSkipsOfflineAgent: reconcile is driven off the
// connect path, but a racing disconnect must not dispatch a job at a node that
// is no longer live — the next reconnect will retry.
func TestReconcileAgentSelfUpdateSkipsOfflineAgent(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()

	if err := srv.updatesSvc.SetPendingAgentUpdate(ctx, "agent-gone", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate() error = %v", err)
	}

	srv.reconcileAgentSelfUpdate(ctx, "agent-gone")

	if got := countSelfUpdateJobs(t, srv); got != 0 {
		t.Fatalf("self-update jobs for an absent agent = %d, want 0", got)
	}
	if _, ok, _ := srv.updatesSvc.PendingAgentUpdate(ctx, "agent-gone"); !ok {
		t.Fatal("an absent agent's pending target must survive for the next reconnect")
	}
}

// selfUpdateJobPayload builds the payload the panel sends for a self-update.
func mustSelfUpdatePayload(t *testing.T, version string) string {
	t.Helper()
	payload, err := buildAgentDirectUpdatePayload("lost-coder/panvex", version)
	if err != nil {
		t.Fatalf("buildAgentDirectUpdatePayload: %v", err)
	}
	return string(payload)
}

// TestSelfUpdateFailuresGiveUpAndStopReenqueueing: a target the node can never
// reach (agent built without version ldflags, 404 asset, bad checksum) must not
// be re-dispatched on every reconnect forever. After the failure budget is
// spent the panel drops the desired state and the reconciler goes quiet.
func TestSelfUpdateFailuresGiveUpAndStopReenqueueing(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()

	srv.seedLiveAgentKeyed("agent-su", Agent{ID: "agent-su", NodeName: "su-node", Version: "1.3.0"})
	if err := srv.updatesSvc.SetPendingAgentUpdate(ctx, "agent-su", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate: %v", err)
	}

	for attempt := 1; attempt <= updates.MaxPendingAgentUpdateFailures; attempt++ {
		job, err := srv.jobs.Enqueue(ctx,
			agentSelfUpdateJobInput("agent-su", mustSelfUpdatePayload(t, "1.4.0"), selfUpdateReconcileActor), srv.now())
		if err != nil {
			t.Fatalf("Enqueue attempt %d: %v", attempt, err)
		}
		srv.RecordClientJobResult(ctx, "agent-su", job.ID, false, "download failed: 404", "", srv.now())

		_, stillPending, _ := srv.updatesSvc.PendingAgentUpdate(ctx, "agent-su")
		wantPending := attempt < updates.MaxPendingAgentUpdateFailures
		if stillPending != wantPending {
			t.Fatalf("after failure %d: pending=%v, want %v", attempt, stillPending, wantPending)
		}
	}

	// Budget spent: a fresh reconnect must not resurrect the doomed job.
	before := countSelfUpdateJobs(t, srv)
	srv.mu.Lock()
	delete(srv.selfUpdateReenqueuedAt, "agent-su") // ignore the throttle for this assertion
	srv.mu.Unlock()
	srv.reconcileAgentSelfUpdate(ctx, "agent-su")
	if after := countSelfUpdateJobs(t, srv); after != before {
		t.Fatalf("reconcile after give-up enqueued %d new job(s); want none", after-before)
	}
}

// TestSelfUpdateSuccessDoesNotConsumeFailureBudget: only failures count. A
// successful delivery leaves the target in place (it clears on the reconnect
// that observes the new version), with its budget untouched.
func TestSelfUpdateSuccessDoesNotConsumeFailureBudget(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()

	srv.seedLiveAgentKeyed("agent-su", Agent{ID: "agent-su", NodeName: "su-node", Version: "1.3.0"})
	if err := srv.updatesSvc.SetPendingAgentUpdate(ctx, "agent-su", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate: %v", err)
	}

	for range updates.MaxPendingAgentUpdateFailures + 2 {
		job, err := srv.jobs.Enqueue(ctx,
			agentSelfUpdateJobInput("agent-su", mustSelfUpdatePayload(t, "1.4.0"), selfUpdateReconcileActor), srv.now())
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		srv.RecordClientJobResult(ctx, "agent-su", job.ID, true, "self-update applied", "", srv.now())
	}

	if _, ok, _ := srv.updatesSvc.PendingAgentUpdate(ctx, "agent-su"); !ok {
		t.Fatal("successful deliveries must not exhaust the failure budget")
	}
}

// TestClientJobFailureDoesNotTouchSelfUpdateBudget: the result hook sees EVERY
// job result, so it must key off the action — a failing client.delete must not
// spend the self-update budget.
func TestClientJobFailureDoesNotTouchSelfUpdateBudget(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()

	srv.seedLiveAgentKeyed("agent-su", Agent{ID: "agent-su", NodeName: "su-node", Version: "1.3.0"})
	if err := srv.updatesSvc.SetPendingAgentUpdate(ctx, "agent-su", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate: %v", err)
	}

	for range updates.MaxPendingAgentUpdateFailures {
		job, err := srv.jobs.Enqueue(ctx, jobs.CreateJobInput{
			Action:         jobs.ActionClientDelete,
			TargetAgentIDs: []string{"agent-su"},
			PayloadJSON:    `{"client_id":"c1","name":"alice"}`,
			ActorID:        "user-1",
		}, srv.now())
		if err != nil {
			t.Fatalf("Enqueue client job: %v", err)
		}
		srv.RecordClientJobResult(ctx, "agent-su", job.ID, false, "telemt unreachable", "", srv.now())
	}

	if _, ok, _ := srv.updatesSvc.PendingAgentUpdate(ctx, "agent-su"); !ok {
		t.Fatal("client-job failures must not consume the self-update budget")
	}
}
