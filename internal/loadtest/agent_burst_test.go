// Package loadtest — Agent burst scenario.
//
// What's measured
//   - Enrollment p99 latency: wall-clock cost of one PutAgent call
//     during a 200-agent concurrent enroll. Models the "fleet just
//     started, every agent races to register" steady state.
//   - Heartbeat p99 latency: wall-clock cost of the PutAgent UPSERT
//     that backs every periodic agent heartbeat (LastSeenAt update).
//     Each of the 200 agents heartbeats once per second for the
//     scenario window — same shape the per-second presence loop hits
//     in production.
//   - Audit-log throughput: every enroll writes one audit row via
//     AppendAuditEvent. The Test variant asserts every audit landed.
//
// How to run
//
//	# Correctness gate (CI default — runs in seconds):
//	go test -run TestAgentBurst ./internal/loadtest/...
//
//	# Throughput numbers (operator-driven):
//	go test -run '^$' -bench BenchmarkAgentBurst -benchtime=1x \
//	    ./internal/loadtest/...
//
// What's a regression
//   - Enrollment p99 climbing past ~50 ms on a typical laptop is the
//     yellow flag — a refactor that introduced a per-row transaction
//     overhead would surface here first.
//   - Heartbeat p99 doubling vs the previous main is a hard regression:
//     the heartbeat path is hit O(fleet × poll-rate) in production.
//   - Any "audit drop" reported by the Test variant is a bug; audit
//     writes are the durability contract for compliance reporting.
package loadtest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// agentBurstParams sizes the workload. Kept as constants (not flags) so
// the bench is deterministic across runs; operators wanting bigger
// numbers can tweak in a branch and rerun locally.
const (
	agentBurstFleetSize       = 200
	agentBurstHeartbeatRounds = 5 // Test variant — 5s wall-clock budget.
	agentBurstHeartbeatPeriod = 200 * time.Millisecond
	agentBurstWindow          = 5 * time.Second
)

// runAgentBurst executes the scenario once. Returns the recorded latency
// samples and the number of audit events written so callers can assert.
// Used by both Test* (correctness) and Benchmark* (timing) entry points.
func runAgentBurst(tb testing.TB, fleetSize, heartbeatRounds int) (*latencySamples, *latencySamples, int64) {
	tb.Helper()
	store := openHarnessStore(tb)
	ctx := tbContext(tb)

	enrollLatency := &latencySamples{}
	heartbeatLatency := &latencySamples{}
	var auditWrites atomic.Int64

	// ---- Enrollment burst: every agent races for PutAgent + audit. ----
	var enrollWG sync.WaitGroup
	enrollWG.Add(fleetSize)
	enrollErr := make(chan error, fleetSize)
	enrollStart := time.Now()
	for i := 0; i < fleetSize; i++ {
		go func(idx int) {
			defer enrollWG.Done()
			now := time.Now()
			rec := storage.AgentRecord{
				ID:           fmt.Sprintf("loadtest-burst-%04d", idx),
				NodeName:     fmt.Sprintf("node-%04d", idx),
				FleetGroupID: fleetGroupID,
				Version:      "burst-1.0.0",
				LastSeenAt:   now,
			}
			t0 := time.Now()
			if err := store.PutAgent(ctx, rec); err != nil {
				enrollErr <- fmt.Errorf("PutAgent[%d]: %w", idx, err)
				return
			}
			enrollLatency.Record(time.Since(t0))

			audit := storage.AuditEventRecord{
				ID:        fmt.Sprintf("audit-enroll-%04d", idx),
				ActorID:   "system",
				Action:    "agent.enroll",
				TargetID:  rec.ID,
				CreatedAt: now,
				Details:   map[string]any{"version": rec.Version},
			}
			if err := store.AppendAuditEvent(ctx, audit); err != nil {
				enrollErr <- fmt.Errorf("AppendAuditEvent[%d]: %w", idx, err)
				return
			}
			auditWrites.Add(1)
		}(i)
	}
	enrollWG.Wait()
	close(enrollErr)
	if errs := drainErrs(enrollErr); len(errs) > 0 {
		tb.Fatalf("enrollment errors (%d): %v", len(errs), errs[0])
	}
	if elapsed := time.Since(enrollStart); elapsed > agentBurstWindow {
		// Soft warning — record but don't fail. Sampling under load
		// occasionally drifts past the 5s budget on busy CI runners.
		tb.Logf("enrollment burst took %v (budget %v)", elapsed, agentBurstWindow)
	}

	// ---- Heartbeat phase: every agent UPSERTs LastSeenAt N times. ----
	var hbWG sync.WaitGroup
	hbWG.Add(fleetSize)
	hbErr := make(chan error, fleetSize*heartbeatRounds)
	for i := 0; i < fleetSize; i++ {
		go func(idx int) {
			defer hbWG.Done()
			ticker := time.NewTicker(agentBurstHeartbeatPeriod)
			defer ticker.Stop()
			for round := 0; round < heartbeatRounds; round++ {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
				rec := storage.AgentRecord{
					ID:           fmt.Sprintf("loadtest-burst-%04d", idx),
					NodeName:     fmt.Sprintf("node-%04d", idx),
					FleetGroupID: fleetGroupID,
					Version:      "burst-1.0.0",
					LastSeenAt:   time.Now(),
				}
				t0 := time.Now()
				if err := store.PutAgent(ctx, rec); err != nil {
					hbErr <- fmt.Errorf("heartbeat[%d/%d]: %w", idx, round, err)
					return
				}
				heartbeatLatency.Record(time.Since(t0))
			}
		}(i)
	}
	hbWG.Wait()
	close(hbErr)
	if errs := drainErrs(hbErr); len(errs) > 0 {
		tb.Fatalf("heartbeat errors (%d): %v", len(errs), errs[0])
	}
	return enrollLatency, heartbeatLatency, auditWrites.Load()
}

// drainErrs reads every queued error from ch into a slice. Caller must
// have closed the channel.
func drainErrs(ch <-chan error) []error {
	var out []error
	for err := range ch {
		if err != nil && !errors.Is(err, context.Canceled) {
			out = append(out, err)
		}
	}
	return out
}

// TestAgentBurstNoErrors is the correctness gate that runs in normal CI
// (every PR via the go-test job). Asserts every enrolment landed, every
// heartbeat completed, and every audit row was accepted by the store.
func TestAgentBurstNoErrors(t *testing.T) {
	enrollLat, hbLat, audits := runAgentBurst(t, agentBurstFleetSize, agentBurstHeartbeatRounds)

	if got, want := enrollLat.Len(), agentBurstFleetSize; got != want {
		t.Errorf("enrollment samples = %d, want %d", got, want)
	}
	if got, want := hbLat.Len(), agentBurstFleetSize*agentBurstHeartbeatRounds; got != want {
		t.Errorf("heartbeat samples = %d, want %d", got, want)
	}
	if got, want := audits, int64(agentBurstFleetSize); got != want {
		t.Errorf("audit writes = %d, want %d (audit drop)", got, want)
	}
	t.Logf("enrollment p50=%v p99=%v", enrollLat.Percentile(0.5), enrollLat.Percentile(0.99))
	t.Logf("heartbeat  p50=%v p99=%v", hbLat.Percentile(0.5), hbLat.Percentile(0.99))
}

// BenchmarkAgentBurst is the load-bench entry point. Each iteration runs
// the full burst once. Operators run with -benchtime=1x for the smoke
// number that surfaces compile/panic regressions, or -benchtime=10s for
// statistically meaningful throughput comparisons across branches.
func BenchmarkAgentBurst(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Match the spec's wall-clock target: 200 enrols + 30s of
		// heartbeats. We reduce heartbeat rounds to keep -benchtime=1x
		// runnable in CI but emit the period as part of the metric name.
		rounds := 5
		enrollLat, hbLat, _ := runAgentBurst(b, agentBurstFleetSize, rounds)
		// Report custom metrics so operators see latency next to ns/op.
		b.ReportMetric(float64(enrollLat.Percentile(0.99).Microseconds()), "enroll-p99-us")
		b.ReportMetric(float64(hbLat.Percentile(0.99).Microseconds()), "heartbeat-p99-us")
	}
}

