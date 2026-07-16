package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
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
