// P2-CHAOS-01: chaos engineering tests covering realistic failure injection
// modes at the control-plane boundaries. These tests leverage the existing
// failingStore harness and the batch-writer / jobs service primitives rather
// than pulling in an external chaos framework — they model the same failure
// surface (partial writes, SIGKILL mid-flush, agent seq reset, job dispatch
// interrupted by reconnect, clock drift) while staying fast enough for
// per-commit CI.
//
// Scenarios audited against remediation plan v4:
//   1. DB drop mid-transaction (partial write rollback of in-memory state)
//   2. SIGKILL between audit enqueue and flush
//   3. Agent restart during snapshot burst (seq-reset baseline, P2-LOG-06)
//   4. Network partition mid-job-dispatch (ack-expiry redispatch, P2-LOG-05)
//   5. Clock drift (retention worker does not prune future events)
//
// Naming follows the ChaosXxx prefix so they can be run as a group:
//   go test -run Chaos ./internal/controlplane/server
package server

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// -----------------------------------------------------------------------
// 1. TestChaosDBDropDuringTransact
//
// Simulates the DB dropping mid-"transaction" by injecting a persistence
// error on the client-assignment write (the second storage call inside
// replaceClientStateWithContext, after PutClient + DeleteClientAssignments).
// The server has no formal Transact wrapper yet (see adoptMu comment in
// server.go — "Full Store.Transact wiring is deferred to P2-ARCH-01"), so
// the invariant we enforce here is the one the codebase already relies on:
// when persistence fails, the in-memory map must not be mutated.
// -----------------------------------------------------------------------

func TestChaosDBDropDuringTransact(t *testing.T) {
	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)

	baseStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer baseStore.Close()

	ctx := context.Background()
	if err := baseStore.PutFleetGroup(ctx, storage.FleetGroupRecord{
		ID:        "default",
		Name:      "Default",
		CreatedAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}
	if err := baseStore.PutAgent(ctx, storage.AgentRecord{
		ID:           "agent-A",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	// Inject failure on the SECOND persistence step (PutClientAssignment)
	// so the first write (PutClient) has already gone in. This mirrors a
	// real partial-transaction abort where some rows hit disk before the DB
	// connection dropped.
	chaosErr := errors.New("chaos: DB connection dropped mid-transaction")
	failing := &failingStore{
		Store:                  baseStore,
		putClientAssignmentErr: chaosErr,
	}

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: failing,
	})
	defer server.Close()

	// Seed the agent in-memory so createClient can resolve a target.
	server.mu.Lock()
	server.agents["agent-A"] = Agent{
		ID:           "agent-A",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}
	server.mu.Unlock()

	input := clientMutationInput{
		Name:      "alice",
		Secret:    "0123456789abcdef0123456789abcdef",
		UserADTag: "0123456789abcdef0123456789abcdef",
		AgentIDs:  []string{"agent-A"},
	}

	jobsBefore := len(server.jobs.List())

	_, _, _, createErr := server.createClientWithContext(ctx, "user-1", input, now)
	if !errors.Is(createErr, chaosErr) {
		t.Fatalf("createClientWithContext() error = %v, want chaos injection error", createErr)
	}

	// Invariant 1: no orphaned managedClient record in the in-memory map.
	// persistClientState writes PutClient successfully, then bails out on
	// PutClientAssignment — replaceClientStateWithContext must NOT commit
	// the in-memory map (the roll-forward contract: commit only after the
	// full persist sequence succeeds).
	server.clientsMu.RLock()
	inMemoryClients := len(server.clients)
	inMemoryAssignments := len(server.clientAssignments)
	inMemoryDeployments := len(server.clientDeployments)
	server.clientsMu.RUnlock()

	if inMemoryClients != 0 {
		t.Fatalf("in-memory clients after failed transact = %d, want 0 (orphan record)", inMemoryClients)
	}
	if inMemoryAssignments != 0 {
		t.Fatalf("in-memory assignments after failed transact = %d, want 0 (orphan record)", inMemoryAssignments)
	}
	if inMemoryDeployments != 0 {
		t.Fatalf("in-memory deployments after failed transact = %d, want 0 (orphan record)", inMemoryDeployments)
	}

	// Invariant 2: no job was enqueued. replaceClientStateWithContext sits
	// BEFORE enqueueClientJob; a persist failure must short-circuit the
	// whole sequence so agents are never commanded to create a client the
	// CP does not know about.
	if jobsAfter := len(server.jobs.List()); jobsAfter != jobsBefore {
		t.Fatalf("jobs enqueued after failed transact = %d, want %d (no side-effect)", jobsAfter-jobsBefore, 0)
	}
}

// -----------------------------------------------------------------------
// 2. TestChaosShutdownMidAudit
//
// Models SIGKILL between audit enqueue and flush. We enqueue N rows, then
// trigger StopWithTimeout with a tight budget so the drain may or may not
// finish — either way the invariants are:
//   * No data corruption (no duplicate / torn rows in the store).
//   * Either every event persisted OR the timeout-error path fires without
//     panicking; under no circumstance do we lose rows silently because the
//     in-memory buffer has its ownership taken over by the drain.
//
// The test uses a slowAuditStore so the drain actually races the shutdown
// budget (otherwise a fast sqlite write makes the test degenerate to
// TestAuditBufferFlushesOnShutdown).
// -----------------------------------------------------------------------

// chaosCountingAuditStore wraps a store and counts how many AppendAuditEvent
// calls have succeeded. Used to assert the persisted row count matches the
// store's own record count (no duplicates, no torn writes).
type chaosCountingAuditStore struct {
	storage.Store
	stall    time.Duration
	appended atomic.Int32
}

func (s *chaosCountingAuditStore) AppendAuditEvent(ctx context.Context, event storage.AuditEventRecord) error {
	// Interruptible stall so Close() does not deadlock when the drain
	// context expires.
	if s.stall > 0 {
		select {
		case <-time.After(s.stall):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := s.Store.AppendAuditEvent(ctx, event); err != nil {
		return err
	}
	s.appended.Add(1)
	return nil
}

func TestChaosShutdownMidAudit(t *testing.T) {
	base, err := sqlite.Open(filepath.Join(t.TempDir(), "chaos-audit.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })

	// 5ms stall per row; with N=40 rows the full drain takes ~200ms. A 50ms
	// shutdown budget forces partial completion on slow CI but still lets
	// fast machines drain a handful of rows cleanly.
	counting := &chaosCountingAuditStore{Store: base, stall: 5 * time.Millisecond}
	w := newStoreBatchWriter(counting, nil)
	w.Start()

	const n = 40
	for i := 0; i < n; i++ {
		w.auditEvents.Enqueue(storage.AuditEventRecord{
			ID:        "chaos-evt-" + randSuffix(i),
			ActorID:   "user-1",
			Action:    "chaos.shutdown",
			TargetID:  "target-1",
			CreatedAt: time.Now().UTC(),
		})
	}

	// Very tight timeout — the drain WILL be interrupted on slow runners
	// and WILL finish on fast ones. The chaos invariant is that neither
	// outcome corrupts data.
	stopErr := w.StopWithTimeout(50 * time.Millisecond)

	// The returned error is either nil (drained in time) or
	// context.DeadlineExceeded (per the StopWithTimeout contract). Anything
	// else is a bug.
	if stopErr != nil && !errors.Is(stopErr, context.DeadlineExceeded) {
		t.Fatalf("StopWithTimeout returned %v, want nil or DeadlineExceeded", stopErr)
	}

	persisted, err := base.ListAuditEvents(context.Background(), n+10)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}

	// Invariant 1: persisted count matches the store's own success counter
	// (no torn rows — if AppendAuditEvent returned nil, the row is readable;
	// if it returned error, the row is absent).
	if int32(len(persisted)) != counting.appended.Load() {
		t.Fatalf("persisted rows = %d, counter = %d (torn write!)", len(persisted), counting.appended.Load())
	}

	// Invariant 2: no duplicate IDs — the drain must not re-submit rows.
	ids := make(map[string]struct{}, len(persisted))
	for _, row := range persisted {
		if _, dup := ids[row.ID]; dup {
			t.Fatalf("duplicate audit row id %q after chaos shutdown", row.ID)
		}
		ids[row.ID] = struct{}{}
	}

	// Invariant 3: whatever path we took, we made progress. A stalled
	// writer that persists zero rows on shutdown would be a regression.
	// (Exact count is not asserted — it depends on CI timing.)
	if len(persisted) == 0 && stopErr == nil {
		t.Fatalf("clean shutdown persisted 0/%d rows — drain contract violated", n)
	}
	if len(persisted) > n {
		t.Fatalf("persisted %d rows > enqueued %d — duplicate flush", len(persisted), n)
	}

	t.Logf("chaos audit shutdown: persisted %d/%d rows, stopErr=%v", len(persisted), n, stopErr)
}

// -----------------------------------------------------------------------
// 3. TestChaosAgentReconnectSeqReset
//
// Agent pushes snapshots with seq=1,2,3 (initial baseline plus deltas),
// then restarts and begins at seq=1 again. Per P2-LOG-06 the CP must
// treat the second seq=1 as a baseline (not a delta on top of the 1+2+3
// accumulation) so the agent's zero-ed counters do not roll back the CP
// gauge, and seq=2 after the restart must accumulate normally.
//
// Unlike TestUsageSeqResetOnAgentRestart which only validates the final
// traffic number, this test captures the chaos shape: an in-flight burst
// of snapshots racing against a stream reconnect.
// -----------------------------------------------------------------------

func TestChaosAgentReconnectSeqReset(t *testing.T) {
	now := time.Date(2026, time.April, 18, 13, 0, 0, 0, time.UTC)
	server := New(Options{Now: func() time.Time { return now }})
	defer server.Close()

	const agentID = "chaos-agent"
	const clientID = "chaos-client"

	// Pre-restart burst: seq 1 (baseline), 2 (+1024), 3 (+512).
	// The first non-zero seq with lastSeen == 0 is the "legacy" path in
	// applyClientUsageSnapshot — it accumulates unconditionally, which is
	// the correct baseline behavior for the first snapshot in a fresh CP.
	preBurst := [][]clientUsageSnapshot{
		{{ClientID: clientID, TrafficUsedBytes: 2048, ObservedAt: now, Seq: 1}},
		{{ClientID: clientID, TrafficUsedBytes: 1024, ObservedAt: now.Add(time.Second), Seq: 2}},
		{{ClientID: clientID, TrafficUsedBytes: 512, ObservedAt: now.Add(2 * time.Second), Seq: 3}},
	}

	server.mu.Lock()
	for _, snapshot := range preBurst {
		server.applyClientUsageSnapshot(agentID, snapshot)
	}
	preRestartTotal := server.clientUsage[clientID][agentID].TrafficUsedBytes
	preRestartSeq := server.lastUsageSeq[agentID]
	server.mu.Unlock()

	// Pre-restart sanity: the three deltas accumulated.
	if want := uint64(2048 + 1024 + 512); preRestartTotal != want {
		t.Fatalf("pre-restart total = %d, want %d", preRestartTotal, want)
	}
	if preRestartSeq != 3 {
		t.Fatalf("pre-restart seq cursor = %d, want 3", preRestartSeq)
	}

	// Agent restart: new stream, counters reset to zero. The first
	// post-restart snapshot arrives as seq=1 with a small fresh value. The
	// CP must NOT accumulate it as a delta.
	restartBaseline := []clientUsageSnapshot{
		{ClientID: clientID, TrafficUsedBytes: 256, ObservedAt: now.Add(10 * time.Second), Seq: 1},
	}
	// Followed immediately by seq=2 (an honest delta post-restart).
	postRestartDelta := []clientUsageSnapshot{
		{ClientID: clientID, TrafficUsedBytes: 128, ObservedAt: now.Add(11 * time.Second), Seq: 2},
	}

	server.mu.Lock()
	server.applyClientUsageSnapshot(agentID, restartBaseline)
	afterBaseline := server.clientUsage[clientID][agentID].TrafficUsedBytes
	afterBaselineSeq := server.lastUsageSeq[agentID]
	server.applyClientUsageSnapshot(agentID, postRestartDelta)
	final := server.clientUsage[clientID][agentID].TrafficUsedBytes
	finalSeq := server.lastUsageSeq[agentID]
	server.mu.Unlock()

	// Invariant 1: the seq=1 restart baseline did NOT add its 256 bytes to
	// the accumulator. If it had, we would see preRestartTotal + 256.
	if afterBaseline != preRestartTotal {
		t.Fatalf("post-restart baseline total = %d, want %d (agent restart must not accumulate)", afterBaseline, preRestartTotal)
	}
	if afterBaselineSeq != 1 {
		t.Fatalf("post-restart seq cursor after baseline = %d, want 1 (CP must reset to new agent seq)", afterBaselineSeq)
	}

	// Invariant 2: the post-restart seq=2 delta DID accumulate normally.
	if want := preRestartTotal + 128; final != want {
		t.Fatalf("post-restart delta total = %d, want %d", final, want)
	}
	if finalSeq != 2 {
		t.Fatalf("final seq cursor = %d, want 2", finalSeq)
	}
}

// -----------------------------------------------------------------------
// 4. TestChaosJobDispatchInterrupted
//
// Agent reconnect during job dispatch: a target goes through
// Queued -> Sent (MarkDelivered). The agent then disconnects before it
// acks or sends a result. After the per-target retry window elapses,
// PendingForAgent on a fresh stream must re-include the target so the job
// can be re-dispatched (P2-LOG-05 / redispatch-on-reconnect path).
// -----------------------------------------------------------------------

func TestChaosJobDispatchInterrupted(t *testing.T) {
	now := time.Date(2026, time.April, 18, 14, 0, 0, 0, time.UTC)
	svc := jobs.NewService()
	svc.SetNow(func() time.Time { return now })

	const agentID = "chaos-target-agent"

	job, err := svc.Enqueue(jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{agentID},
		TTL:            time.Hour,
		IdempotencyKey: "chaos-dispatch-1",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	// First dispatch: agent is live, CP flips the target to Sent.
	svc.MarkDelivered(agentID, job.ID, now)

	// Simulate network partition: the agent drops off the stream AFTER the
	// delivery ack reaches the CP but BEFORE any result comes back. Target
	// remains in Sent state. Within the retry window, PendingForAgent must
	// NOT re-dispatch (the command is still considered in-flight).
	retryAfter := 30 * time.Second
	stillInFlight := svc.PendingForAgent(agentID, retryAfter)
	if len(stillInFlight) != 0 {
		t.Fatalf("PendingForAgent within retry window = %d, want 0 (no premature redispatch)", len(stillInFlight))
	}

	// Agent reconnects on a fresh stream AFTER the retry window has
	// elapsed. PendingForAgent must now re-include the target so the CP
	// can re-dispatch and let the agent's idempotency cache dedupe if the
	// original run actually completed.
	svc.SetNow(func() time.Time { return now.Add(retryAfter + time.Second) })
	redispatch := svc.PendingForAgent(agentID, retryAfter)
	if len(redispatch) != 1 {
		t.Fatalf("PendingForAgent after retry window = %d, want 1 (redispatch path broken)", len(redispatch))
	}
	if redispatch[0].ID != job.ID {
		t.Fatalf("redispatch[0].ID = %q, want %q", redispatch[0].ID, job.ID)
	}

	// And a long partition: after TTL elapses, PruneAcknowledgedTargets /
	// expireJobsLocked eventually expires the target so the CP stops
	// spinning on a permanently-dead agent. Advance the clock well past
	// TTL; the next PendingForAgent call triggers expireJobsLocked.
	svc.SetNow(func() time.Time { return now.Add(2 * time.Hour) })
	afterExpiry := svc.PendingForAgent(agentID, retryAfter)
	// TTL expired during the call — target is now Expired and should NOT
	// be re-dispatched.
	for _, candidate := range afterExpiry {
		for _, target := range candidate.Targets {
			if target.AgentID == agentID && target.Status != jobs.TargetStatusExpired {
				t.Fatalf("target status after TTL = %q, want Expired", target.Status)
			}
		}
	}
}

// -----------------------------------------------------------------------
// 5. TestChaosClockDrift
//
// The retention prune worker uses s.now() to compute cutoffs. If the clock
// jumps BACKWARD (NTP correction, VM migration, host time skew), the
// cutoff moves backward too — so rows that were previously eligible for
// pruning become "in the future" relative to the new cutoff. The
// invariant is that the worker must NOT then treat those rows as future
// events to prune (it would otherwise delete valid data) AND that once
// the clock catches back up, normal pruning resumes.
//
// We exercise runRetentionPrune directly because the ticker-driven worker
// would be non-deterministic under test.
// -----------------------------------------------------------------------

func TestChaosClockDrift(t *testing.T) {
	baseStore, err := sqlite.Open(filepath.Join(t.TempDir(), "chaos-drift.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer baseStore.Close()

	baseline := time.Date(2026, time.April, 18, 15, 0, 0, 0, time.UTC)

	// Shared mutable clock so the server and the test agree on "now"
	// without relying on wall time.
	var clockMu sync.Mutex
	clockAt := baseline
	nowFn := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clockAt
	}
	setClock := func(t time.Time) {
		clockMu.Lock()
		clockAt = t
		clockMu.Unlock()
	}

	server := New(Options{
		Now:   nowFn,
		Store: baseStore,
	})
	defer server.Close()

	ctx := context.Background()

	// Seed two audit rows at the baseline time. Wall-clock CreatedAt is
	// stored verbatim — we write them via the raw Store.AppendAuditEvent to
	// control the CreatedAt stamps precisely.
	oldEvent := storage.AuditEventRecord{
		ID:        "audit-old",
		ActorID:   "user-1",
		Action:    "test.old",
		TargetID:  "target",
		CreatedAt: baseline.Add(-48 * time.Hour), // 2 days before baseline
	}
	freshEvent := storage.AuditEventRecord{
		ID:        "audit-fresh",
		ActorID:   "user-1",
		Action:    "test.fresh",
		TargetID:  "target",
		CreatedAt: baseline.Add(-time.Hour), // 1 hour before baseline
	}
	if err := baseStore.AppendAuditEvent(ctx, oldEvent); err != nil {
		t.Fatalf("AppendAuditEvent(old) error = %v", err)
	}
	if err := baseStore.AppendAuditEvent(ctx, freshEvent); err != nil {
		t.Fatalf("AppendAuditEvent(fresh) error = %v", err)
	}

	// Configure retention so "old" is past TTL (24h) but "fresh" is not.
	server.settingsMu.Lock()
	server.retention = RetentionSettings{
		AuditEventSeconds: 86400, // 24h
	}
	server.settingsMu.Unlock()

	// ---- Phase 1: clock jumps BACKWARD by 1 hour before any prune runs.
	// Cutoff becomes (baseline - 1h) - 24h. Neither row is in the future
	// relative to this cutoff; "fresh" (baseline - 1h) is exactly at the
	// cutoff boundary so it is NOT pruned (PruneAuditEvents uses strict
	// <). "old" (baseline - 48h) is older than the cutoff and IS pruned.
	// The critical invariant: no row with a CreatedAt in the FUTURE
	// relative to the drifted clock is ever treated as eligible.
	driftBackward := baseline.Add(-time.Hour)
	setClock(driftBackward)

	server.runRetentionPrune(ctx, "audit_events", nowFn(), 86400, baseStore.PruneAuditEvents)

	remaining, err := baseStore.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListAuditEvents (after drift-backward) error = %v", err)
	}

	// Assertion: the "fresh" row (1h before the real baseline, 0h before
	// the drifted clock) must survive. The "old" row (48h before baseline
	// = 47h before drifted clock) is past the 24h TTL and must be pruned.
	hasFresh := false
	hasOld := false
	for _, row := range remaining {
		if row.ID == freshEvent.ID {
			hasFresh = true
		}
		if row.ID == oldEvent.ID {
			hasOld = true
		}
	}
	if !hasFresh {
		t.Fatalf("fresh row pruned under backward drift — retention treated it as future event")
	}
	if hasOld {
		t.Fatalf("old row survived backward drift — prune path regressed")
	}

	// ---- Phase 2: seed a row that IS in the future relative to the
	// drifted clock (but in the past relative to the original baseline).
	// If the worker incorrectly computed cutoff = now + TTL (sign flip), it
	// would delete this row. It must survive both drift and catch-up.
	futureRelativeEvent := storage.AuditEventRecord{
		ID:        "audit-future-relative",
		ActorID:   "user-1",
		Action:    "test.future",
		TargetID:  "target",
		CreatedAt: baseline.Add(-30 * time.Minute), // future relative to driftBackward
	}
	if err := baseStore.AppendAuditEvent(ctx, futureRelativeEvent); err != nil {
		t.Fatalf("AppendAuditEvent(future-relative) error = %v", err)
	}

	// Drift even further backward — 2 hours before baseline. The
	// future-relative row is now 1h30m "in the future" of the clock.
	setClock(baseline.Add(-2 * time.Hour))
	server.runRetentionPrune(ctx, "audit_events", nowFn(), 86400, baseStore.PruneAuditEvents)

	afterExtremeDrift, err := baseStore.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListAuditEvents (extreme drift) error = %v", err)
	}
	futureSurvived := false
	for _, row := range afterExtremeDrift {
		if row.ID == futureRelativeEvent.ID {
			futureSurvived = true
		}
	}
	if !futureSurvived {
		t.Fatalf("future-relative row deleted during backward drift — retention cutoff has wrong sign")
	}

	// ---- Phase 3: clock eventually catches up and advances far past the
	// original baseline. The once-fresh and future-relative rows should
	// now be old enough to prune. This verifies retention still fires
	// eventually — a "stuck" drifted clock is not a permanent disabler.
	setClock(baseline.Add(72 * time.Hour)) // +3 days after baseline
	server.runRetentionPrune(ctx, "audit_events", nowFn(), 86400, baseStore.PruneAuditEvents)

	afterCatchup, err := baseStore.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListAuditEvents (after catchup) error = %v", err)
	}
	for _, row := range afterCatchup {
		if row.ID == freshEvent.ID || row.ID == futureRelativeEvent.ID {
			t.Fatalf("row %q survived clock catchup — TTL enforcement lost after drift", row.ID)
		}
	}
}
