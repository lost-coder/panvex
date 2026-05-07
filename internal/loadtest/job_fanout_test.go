// Package loadtest — Job fan-out scenario.
//
// What's measured
//   - Job ingest latency: wall-clock cost of one Enqueue call carrying
//     500 target agent IDs. Production hits this on every "Apply to all
//     nodes" operator action; the per-target index bookkeeping
//     (syncJobTargetsIndexLocked) grows linearly with the fan-out.
//   - Completion time: end-to-end from Enqueue to all 500 targets in a
//     terminal state (Succeeded). 500 concurrent ack workers race the
//     RecordResult path — this is the closing edge of every fleet
//     rollout.
//   - Audit-log write throughput: every ack writes one audit row. The
//     Test variant counts emitted events and asserts none was dropped.
//
// How to run
//
//	go test -run TestJobFanout ./internal/loadtest/...
//	go test -run '^$' -bench BenchmarkJobFanout -benchtime=1x \
//	    ./internal/loadtest/...
//
// What's a regression
//   - Ingest latency past ~50 ms for a 500-target Enqueue suggests
//     syncJobTargetsIndexLocked or persistJob regressed.
//   - Completion time past ~5 s on a typical laptop is the yellow
//     flag — RecordResult acquires the exclusive lock per call so any
//     contention surfaces here.
//   - Audit drop is a hard failure; audit is the durability contract.
package loadtest

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const (
	jobFanoutTargets = 500
)

// runJobFanout runs the scenario once. Returns the recorded ingest and
// per-ack latencies plus the audit write count.
func runJobFanout(tb testing.TB) (ingest, ack *latencySamples, completion time.Duration, audits int64) {
	tb.Helper()
	store := openHarnessStore(tb)
	ctx := tbContext(tb)

	// Seed the agents table; FK on job_targets requires every agent ID
	// referenced by Enqueue to exist. Bulk insert keeps setup cheap.
	agents := make([]storage.AgentRecord, jobFanoutTargets)
	targetIDs := make([]string, jobFanoutTargets)
	now := time.Now().UTC()
	for i := range agents {
		id := fmt.Sprintf("loadtest-fanout-%04d", i)
		agents[i] = storage.AgentRecord{
			ID:           id,
			NodeName:     fmt.Sprintf("node-fanout-%04d", i),
			FleetGroupID: fleetGroupID,
			Version:      "fanout-1.0.0",
			LastSeenAt:   now,
		}
		targetIDs[i] = id
	}
	if err := store.PutAgentsBulk(ctx, agents); err != nil {
		tb.Fatalf("PutAgentsBulk: %v", err)
	}

	svc := jobs.NewService()
	ingest = &latencySamples{}
	ack = &latencySamples{}

	// ---- Ingest: one operator enqueues the fan-out job. ----
	t0 := time.Now()
	job, err := svc.Enqueue(ctx, jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: targetIDs,
		TTL:            30 * time.Second,
		IdempotencyKey: fmt.Sprintf("loadtest-fanout-%d", time.Now().UnixNano()),
		ActorID:        "loadtest-operator",
	}, now)
	ingest.Record(time.Since(t0))
	if err != nil {
		tb.Fatalf("Enqueue: %v", err)
	}
	if got, want := len(job.Targets), jobFanoutTargets; got != want {
		tb.Fatalf("len(job.Targets) = %d, want %d", got, want)
	}

	// ---- Ack + audit phase: every target reports Success concurrently. ----
	var auditWrites atomic.Int64
	completionStart := time.Now()
	var wg sync.WaitGroup
	wg.Add(jobFanoutTargets)
	auditErr := make(chan error, jobFanoutTargets)
	for i, agentID := range targetIDs {
		go func(idx int, agentID string) {
			defer wg.Done()
			ackedAt := time.Now()
			svc.MarkAcknowledged(ctx, agentID, job.ID, ackedAt)
			t1 := time.Now()
			ok := svc.RecordResult(ctx, agentID, job.ID, true /*success*/, "ok", "{}", t1)
			if !ok {
				// Don't sample the latency for failed paths — otherwise
				// ack.Len() == jobFanoutTargets stays true while
				// auditErr counts the same failure, confusing diagnostics.
				auditErr <- fmt.Errorf("RecordResult[%d]: target not found", idx)
				return
			}
			ack.Record(time.Since(t1))
			audit := storage.AuditEventRecord{
				ID:        fmt.Sprintf("audit-fanout-%04d", idx),
				ActorID:   agentID,
				Action:    "job.result",
				TargetID:  job.ID,
				CreatedAt: t1,
				Details:   map[string]any{"success": true},
			}
			if err := store.AppendAuditEvent(ctx, audit); err != nil {
				auditErr <- fmt.Errorf("AppendAuditEvent[%d]: %w", idx, err)
				return
			}
			auditWrites.Add(1)
		}(i, agentID)
	}
	wg.Wait()
	close(auditErr)
	if errs := drainErrs(auditErr); len(errs) > 0 {
		tb.Fatalf("ack/audit errors (%d): %v", len(errs), errs[0])
	}

	// Wait until every target has transitioned out of pending state.
	// PendingForAgent uses retryAfter=0 so any target still queued or
	// sent gets re-included; success/failed/expired do not.
	allTerminal := func() bool {
		view, ok := svc.Get(job.ID)
		if !ok {
			return false
		}
		for _, t := range view.Targets {
			if t.Status != jobs.TargetStatusSucceeded &&
				t.Status != jobs.TargetStatusFailed &&
				t.Status != jobs.TargetStatusExpired {
				return false
			}
		}
		return true
	}
	if !eventually(tb, 10*time.Second, allTerminal) {
		view, _ := svc.Get(job.ID)
		var nonTerminal int
		for _, t := range view.Targets {
			if t.Status != jobs.TargetStatusSucceeded {
				nonTerminal++
			}
		}
		tb.Fatalf("job did not complete: %d/%d targets non-terminal", nonTerminal, jobFanoutTargets)
	}
	completion = time.Since(completionStart)
	return ingest, ack, completion, auditWrites.Load()
}

// TestJobFanoutCompletes is the correctness gate.
func TestJobFanoutCompletes(t *testing.T) {
	ingest, ack, completion, audits := runJobFanout(t)

	if got, want := ingest.Len(), 1; got != want {
		t.Errorf("ingest samples = %d, want %d", got, want)
	}
	if got, want := ack.Len(), jobFanoutTargets; got != want {
		t.Errorf("ack samples = %d, want %d", got, want)
	}
	if got, want := audits, int64(jobFanoutTargets); got != want {
		t.Errorf("audit writes = %d, want %d (audit drop)", got, want)
	}
	t.Logf("ingest p99=%v", ingest.Percentile(0.99))
	t.Logf("ack    p50=%v p99=%v", ack.Percentile(0.5), ack.Percentile(0.99))
	t.Logf("completion=%v", completion)
}

// BenchmarkJobFanout is the load-bench entry point.
func BenchmarkJobFanout(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ingest, ack, completion, _ := runJobFanout(b)
		b.ReportMetric(float64(ingest.Percentile(0.99).Microseconds()), "ingest-p99-us")
		b.ReportMetric(float64(ack.Percentile(0.99).Microseconds()), "ack-p99-us")
		b.ReportMetric(float64(completion.Milliseconds()), "completion-ms")
	}
}
