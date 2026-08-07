package server

import (
	"encoding/json"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// countTelemtUpdateJobs returns how many telemt.update jobs are queued.
func countTelemtUpdateJobs(t *testing.T, srv *Server) int {
	t.Helper()
	listed := srv.jobs.ListRecentWithContext(t.Context(), 100)
	n := 0
	for i := range listed {
		if listed[i].Action == jobs.ActionTelemtUpdate {
			n++
		}
	}
	return n
}

// lastTelemtUpdateJob returns the most-recently-listed telemt.update job.
func lastTelemtUpdateJob(t *testing.T, srv *Server) *jobs.Job {
	t.Helper()
	listed := srv.jobs.ListRecentWithContext(t.Context(), 100)
	for i := range listed {
		if listed[i].Action == jobs.ActionTelemtUpdate {
			return &listed[i]
		}
	}
	return nil
}

// seedTelemtUpdateReconcileAgent wires agentID with a binary strategy and an
// available probe, live in the store — the precondition reconcile needs to
// actually dispatch (it re-reads the strategy before every re-enqueue).
func seedTelemtUpdateReconcileAgent(t *testing.T, srv *Server, agentID string) {
	t.Helper()
	srv.seedLiveAgentKeyed(agentID, Agent{
		ID:                agentID,
		NodeName:          agentID + "-node",
		TelemtUpdateProbe: availableBinaryProbe(),
	})
	seedTestAgentRow(t, srv, agentID, srv.now())
	if err := srv.telemtStrategySvc.Put(t.Context(), agentID, updates.Strategy{
		Mode:        updates.StrategyModeBinary,
		RestartSpec: "systemd:telemt",
		BinaryPath:  "/usr/local/bin/telemt",
	}); err != nil {
		t.Fatalf("Put strategy: %v", err)
	}
}

// TestReconcileTelemtUpdateReenqueuesForPendingTarget: the operator asked for
// 3.4.25 while the node was offline, so the original job expired unseen (TTL
// 10m). On reconnect the pending target must be re-dispatched with the
// correct payload, otherwise the update is lost forever (mirrors D-1 for
// agent self-update).
func TestReconcileTelemtUpdateReenqueuesForPendingTarget(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()
	seedTelemtUpdateReconcileAgent(t, srv, "agent-tu")

	if err := srv.updatesSvc.SetPendingTelemtUpdate(ctx, "agent-tu", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate() error = %v", err)
	}

	srv.reconcileTelemtUpdate(ctx, "agent-tu")

	if got := countTelemtUpdateJobs(t, srv); got != 1 {
		t.Fatalf("telemt.update jobs after reconcile = %d, want 1", got)
	}
	job := lastTelemtUpdateJob(t, srv)
	if job == nil {
		t.Fatal("no telemt.update job enqueued")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["version"] != "3.4.25" {
		t.Fatalf("payload version = %v, want 3.4.25", payload["version"])
	}
	if payload["allow_downgrade"] != nil {
		t.Fatalf("reconcile must never request a downgrade automatically, got allow_downgrade=%v", payload["allow_downgrade"])
	}
	if job.ActorID != telemtUpdateReconcileActor {
		t.Fatalf("ActorID = %q, want %q", job.ActorID, telemtUpdateReconcileActor)
	}
	// The target is still outstanding: nothing has reported success yet.
	if _, ok, _ := srv.updatesSvc.PendingTelemtUpdate(ctx, "agent-tu"); !ok {
		t.Fatal("pending target must survive until the job reports success")
	}
}

// TestReconcileTelemtUpdateThrottlesReenqueue: a flapping node that
// reconnects repeatedly inside one job TTL must not accumulate a queue of
// identical telemt.update jobs.
func TestReconcileTelemtUpdateThrottlesReenqueue(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()
	seedTelemtUpdateReconcileAgent(t, srv, "agent-tu")

	if err := srv.updatesSvc.SetPendingTelemtUpdate(ctx, "agent-tu", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate() error = %v", err)
	}

	srv.reconcileTelemtUpdate(ctx, "agent-tu")
	srv.reconcileTelemtUpdate(ctx, "agent-tu")
	srv.reconcileTelemtUpdate(ctx, "agent-tu")

	if got := countTelemtUpdateJobs(t, srv); got != 1 {
		t.Fatalf("telemt.update jobs after three reconnects inside the TTL = %d, want 1", got)
	}
}

// TestReconcileTelemtUpdateNoPendingIsNoOp: the overwhelmingly common case —
// an agent reconnects with nothing pending. No job, no error, no write.
func TestReconcileTelemtUpdateNoPendingIsNoOp(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()
	seedTelemtUpdateReconcileAgent(t, srv, "agent-tu")

	srv.reconcileTelemtUpdate(ctx, "agent-tu")

	if got := countTelemtUpdateJobs(t, srv); got != 0 {
		t.Fatalf("telemt.update jobs without a pending target = %d, want 0", got)
	}
}

// TestReconcileTelemtUpdateSkipsOfflineAgent: reconcile is driven off the
// connect path, but a racing disconnect must not dispatch a job at a node
// that is no longer live — the next reconnect will retry.
func TestReconcileTelemtUpdateSkipsOfflineAgent(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()

	if err := srv.updatesSvc.SetPendingTelemtUpdate(ctx, "agent-gone", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate() error = %v", err)
	}

	srv.reconcileTelemtUpdate(ctx, "agent-gone")

	if got := countTelemtUpdateJobs(t, srv); got != 0 {
		t.Fatalf("telemt.update jobs for an absent agent = %d, want 0", got)
	}
	if _, ok, _ := srv.updatesSvc.PendingTelemtUpdate(ctx, "agent-gone"); !ok {
		t.Fatal("an absent agent's pending target must survive for the next reconnect")
	}
}

// TestReconcileTelemtUpdateSkipsWhenStrategyUnconfigured: the strategy may
// have been cleared/switched away from "binary" since the operator's click —
// reconcile must not dispatch a job the agent would reject, and must leave
// the pending target in place for a future strategy fix.
func TestReconcileTelemtUpdateSkipsWhenStrategyUnconfigured(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()
	srv.seedLiveAgentKeyed("agent-tu", Agent{
		ID:                "agent-tu",
		NodeName:          "tu-node",
		TelemtUpdateProbe: availableBinaryProbe(),
	})
	// No strategy Put at all.

	if err := srv.updatesSvc.SetPendingTelemtUpdate(ctx, "agent-tu", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate() error = %v", err)
	}

	srv.reconcileTelemtUpdate(ctx, "agent-tu")

	if got := countTelemtUpdateJobs(t, srv); got != 0 {
		t.Fatalf("telemt.update jobs with no configured strategy = %d, want 0", got)
	}
	if _, ok, _ := srv.updatesSvc.PendingTelemtUpdate(ctx, "agent-tu"); !ok {
		t.Fatal("pending target must survive a missing strategy for a later fix")
	}
}

// TestTelemtUpdateSuccessClearsPendingTargetAndAudits: the primary result
// path — a successful delivery must clear the pending target immediately
// (unlike agent self-update, telemt.update has no live-version comparison to
// fall back on) and record a success audit entry.
func TestTelemtUpdateSuccessClearsPendingTargetAndAudits(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()
	seedTelemtUpdateReconcileAgent(t, srv, "agent-tu")

	if err := srv.updatesSvc.SetPendingTelemtUpdate(ctx, "agent-tu", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate: %v", err)
	}
	payload, err := buildTelemtUpdateJobPayload(updates.Strategy{
		Mode: updates.StrategyModeBinary, RestartSpec: "systemd:telemt", BinaryPath: "/usr/local/bin/telemt",
	}, "3.4.25", false)
	if err != nil {
		t.Fatalf("buildTelemtUpdateJobPayload: %v", err)
	}
	job, err := srv.jobs.Enqueue(ctx, telemtUpdateJobInput("agent-tu", payload, "user-1"), srv.now())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	srv.RecordClientJobResult(ctx, "agent-tu", job.ID, true, "telemt update applied", "", srv.now())

	if _, ok, _ := srv.updatesSvc.PendingTelemtUpdate(ctx, "agent-tu"); ok {
		t.Fatal("a successful delivery must clear the pending target")
	}
}

// TestTelemtUpdateStaleSuccessDoesNotClearNewerPendingTarget: an operator
// dispatches version A, then clicks again for version B before A's job
// result comes back (e.g. A was slow, or re-dispatched by reconcile). A's
// success report must not clear B's still-outstanding pending target — that
// would silently drop the operator's most recent request. B's own success
// report must clear it.
func TestTelemtUpdateStaleSuccessDoesNotClearNewerPendingTarget(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()
	seedTelemtUpdateReconcileAgent(t, srv, "agent-tu")

	strategy := updates.Strategy{
		Mode: updates.StrategyModeBinary, RestartSpec: "systemd:telemt", BinaryPath: "/usr/local/bin/telemt",
	}
	payloadA, err := buildTelemtUpdateJobPayload(strategy, "3.4.25", false)
	if err != nil {
		t.Fatalf("buildTelemtUpdateJobPayload(A): %v", err)
	}
	jobA, err := srv.jobs.Enqueue(ctx, telemtUpdateJobInput("agent-tu", payloadA, "user-1"), srv.now())
	if err != nil {
		t.Fatalf("Enqueue(A): %v", err)
	}

	// The operator clicks again for a newer version before A's result lands.
	if err := srv.updatesSvc.SetPendingTelemtUpdate(ctx, "agent-tu", "3.4.26"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate(B): %v", err)
	}

	// A's (stale) success result must not clear B's pending target.
	srv.RecordClientJobResult(ctx, "agent-tu", jobA.ID, true, "telemt update applied", "", srv.now())
	if version, ok, _ := srv.updatesSvc.PendingTelemtUpdate(ctx, "agent-tu"); !ok || version != "3.4.26" {
		t.Fatalf("stale success must not disturb the newer pending target, got %q/%v", version, ok)
	}

	payloadB, err := buildTelemtUpdateJobPayload(strategy, "3.4.26", false)
	if err != nil {
		t.Fatalf("buildTelemtUpdateJobPayload(B): %v", err)
	}
	jobB, err := srv.jobs.Enqueue(ctx, telemtUpdateJobInput("agent-tu", payloadB, "user-1"), srv.now())
	if err != nil {
		t.Fatalf("Enqueue(B): %v", err)
	}
	srv.RecordClientJobResult(ctx, "agent-tu", jobB.ID, true, "telemt update applied", "", srv.now())
	if _, ok, _ := srv.updatesSvc.PendingTelemtUpdate(ctx, "agent-tu"); ok {
		t.Fatal("B's own success must clear the pending target")
	}
}

// TestTelemtUpdateFailuresGiveUpAndStopReenqueueing: a target the node can
// never reach (bad release asset, unwritable binary path) must not be
// re-dispatched on every reconnect forever. After the shared failure budget
// is spent the panel drops the desired state and the reconciler goes quiet.
func TestTelemtUpdateFailuresGiveUpAndStopReenqueueing(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()
	seedTelemtUpdateReconcileAgent(t, srv, "agent-tu")

	if err := srv.updatesSvc.SetPendingTelemtUpdate(ctx, "agent-tu", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate: %v", err)
	}
	payload, err := buildTelemtUpdateJobPayload(updates.Strategy{
		Mode: updates.StrategyModeBinary, RestartSpec: "systemd:telemt", BinaryPath: "/usr/local/bin/telemt",
	}, "3.4.25", false)
	if err != nil {
		t.Fatalf("buildTelemtUpdateJobPayload: %v", err)
	}

	for attempt := 1; attempt <= updates.MaxPendingAgentUpdateFailures; attempt++ {
		job, err := srv.jobs.Enqueue(ctx, telemtUpdateJobInput("agent-tu", payload, telemtUpdateReconcileActor), srv.now())
		if err != nil {
			t.Fatalf("Enqueue attempt %d: %v", attempt, err)
		}
		srv.RecordClientJobResult(ctx, "agent-tu", job.ID, false, "download failed: 404", "", srv.now())

		_, stillPending, _ := srv.updatesSvc.PendingTelemtUpdate(ctx, "agent-tu")
		wantPending := attempt < updates.MaxPendingAgentUpdateFailures
		if stillPending != wantPending {
			t.Fatalf("after failure %d: pending=%v, want %v", attempt, stillPending, wantPending)
		}
	}

	// Budget spent: a fresh reconnect must not resurrect the doomed job.
	before := countTelemtUpdateJobs(t, srv)
	srv.mu.Lock()
	delete(srv.telemtUpdateReenqueuedAt, "agent-tu") // ignore the throttle for this assertion
	srv.mu.Unlock()
	srv.reconcileTelemtUpdate(ctx, "agent-tu")
	if after := countTelemtUpdateJobs(t, srv); after != before {
		t.Fatalf("reconcile after give-up enqueued %d new job(s); want none", after-before)
	}
}

// TestClientJobFailureDoesNotTouchTelemtUpdateBudget: the result hook sees
// EVERY job result, so it must key off the action — a failing client.delete
// must not spend the telemt.update budget.
func TestClientJobFailureDoesNotTouchTelemtUpdateBudget(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()
	seedTelemtUpdateReconcileAgent(t, srv, "agent-tu")

	if err := srv.updatesSvc.SetPendingTelemtUpdate(ctx, "agent-tu", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate: %v", err)
	}

	for range updates.MaxPendingAgentUpdateFailures {
		job, err := srv.jobs.Enqueue(ctx, jobs.CreateJobInput{
			Action:         jobs.ActionClientDelete,
			TargetAgentIDs: []string{"agent-tu"},
			PayloadJSON:    `{"client_id":"c1","name":"alice"}`,
			ActorID:        "user-1",
		}, srv.now())
		if err != nil {
			t.Fatalf("Enqueue client job: %v", err)
		}
		srv.RecordClientJobResult(ctx, "agent-tu", job.ID, false, "telemt unreachable", "", srv.now())
	}

	if _, ok, _ := srv.updatesSvc.PendingTelemtUpdate(ctx, "agent-tu"); !ok {
		t.Fatal("client-job failures must not consume the telemt.update budget")
	}
}

// TestSelfUpdateAndTelemtUpdateBudgetsAreIndependent: a failing
// agent.self-update job must not touch the telemt.update budget for the same
// agent, and vice versa — two independent pending maps, two independent
// budgets.
func TestSelfUpdateAndTelemtUpdateBudgetsAreIndependent(t *testing.T) {
	srv, _ := setupTransportModeServer(t)
	ctx := t.Context()
	seedTelemtUpdateReconcileAgent(t, srv, "agent-tu")

	if err := srv.updatesSvc.SetPendingAgentUpdate(ctx, "agent-tu", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate: %v", err)
	}
	if err := srv.updatesSvc.SetPendingTelemtUpdate(ctx, "agent-tu", "3.4.25"); err != nil {
		t.Fatalf("SetPendingTelemtUpdate: %v", err)
	}

	for range updates.MaxPendingAgentUpdateFailures {
		job, err := srv.jobs.Enqueue(ctx,
			agentSelfUpdateJobInput("agent-tu", mustSelfUpdatePayload(t, "1.4.0"), selfUpdateReconcileActor), srv.now())
		if err != nil {
			t.Fatalf("Enqueue self-update: %v", err)
		}
		srv.RecordClientJobResult(ctx, "agent-tu", job.ID, false, "download failed: 404", "", srv.now())
	}

	if _, ok, _ := srv.updatesSvc.PendingAgentUpdate(ctx, "agent-tu"); ok {
		t.Fatal("agent self-update budget should have been spent")
	}
	if _, ok, _ := srv.updatesSvc.PendingTelemtUpdate(ctx, "agent-tu"); !ok {
		t.Fatal("telemt.update pending target must survive an unrelated self-update budget exhaustion")
	}
}
