package jobs

// P2-PERF-05: microbenchmarks for the job service hot paths.
//
// These benchmarks pin the baseline cost of Enqueue and PendingForAgent so
// Phase 3 (PERF-06) changes can be compared against current main. The
// benchmarks run against the in-memory service (no store): they measure the
// validation + bookkeeping cost, not DB latency.
//
// Run:
//
//	go test -bench=. -benchtime=3s -run=^$ -count=1 \
//	    ./internal/controlplane/jobs

import (
	"fmt"
	"testing"
	"time"
)

// BenchmarkServiceEnqueueSingleTarget measures the most common shape of a
// client mutation job: one target agent, one idempotency key. This is the
// path exercised by the HTTP client CRUD handlers.
func BenchmarkServiceEnqueueSingleTarget(b *testing.B) {
	svc := NewService()
	now := time.Now()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Enqueue(CreateJobInput{
			Action:         ActionClientUpdate,
			TargetAgentIDs: []string{"agent-1"},
			TTL:            30 * time.Second,
			IdempotencyKey: fmt.Sprintf("bench-%d", i),
			ActorID:        "user-1",
			PayloadJSON:    `{"client_id":"c1"}`,
		}, now); err != nil {
			b.Fatalf("Enqueue: %v", err)
		}
	}
}

// BenchmarkServiceEnqueueFanOut10 models a fleet-wide rollout to 10 agents.
// The per-target index bookkeeping grows linearly with fan-out, so this is
// the benchmark to watch when tuning syncJobTargetsIndexLocked.
func BenchmarkServiceEnqueueFanOut10(b *testing.B) {
	svc := NewService()
	now := time.Now()
	targets := make([]string, 10)
	for i := range targets {
		targets[i] = fmt.Sprintf("agent-%02d", i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Enqueue(CreateJobInput{
			Action:         ActionRuntimeReload,
			TargetAgentIDs: targets,
			TTL:            30 * time.Second,
			IdempotencyKey: fmt.Sprintf("bench-fanout-%d", i),
			ActorID:        "user-1",
		}, now); err != nil {
			b.Fatalf("Enqueue: %v", err)
		}
	}
}

// BenchmarkServicePendingForAgent measures the read-side hot path the agent
// gRPC stream calls on every poll. After preloading the service with 1000
// jobs, each iteration resolves the slice of jobs pending for one agent.
func BenchmarkServicePendingForAgent(b *testing.B) {
	svc := NewService()
	now := time.Now()

	const preload = 1000
	for i := 0; i < preload; i++ {
		// Round-robin across 10 agents so each agent ends up with ~100 jobs.
		agent := fmt.Sprintf("agent-%02d", i%10)
		if _, err := svc.Enqueue(CreateJobInput{
			Action:         ActionRuntimeReload,
			TargetAgentIDs: []string{agent},
			TTL:            30 * time.Second,
			IdempotencyKey: fmt.Sprintf("preload-%d", i),
			ActorID:        "user-1",
		}, now); err != nil {
			b.Fatalf("Enqueue preload: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = svc.PendingForAgent("agent-00", 0)
	}
}
