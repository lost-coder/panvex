package batchwriter

// P2-PERF-05: microbenchmarks for the control-plane hot paths.
//
// These benchmarks establish a reproducible baseline for the batch writer and
// event hub so PERF-06 (bulk insert) can be compared apples-to-apples against
// the current main. They intentionally avoid any real database I/O: we use
// SQLite on a memfs temp dir and drive enqueue/flush synchronously so the
// numbers reflect the in-process hot path rather than disk latency.
//
// Run locally:
//
//	go test -bench=. -benchtime=3s -run=^$ -count=1 \
//	    ./internal/controlplane/server

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// BenchmarkBatchWriterEnqueue measures the cost of a single Enqueue on the
// agents buffer. Enqueue is on the critical path of every agent snapshot RPC
// so this is the most important microbench for PERF-06 comparison.
//
// The benchmark does NOT flush to the backing store: it only measures the
// in-memory append + signal path. We do not call Start() so the background
// flush loop is not running, and we do not Drain at the end — the buffer is
// simply reset so the next invocation is clean.
func BenchmarkBatchWriterEnqueue(b *testing.B) {
	store, err := sqlite.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("sqlite.Open: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })

	w := New(store, nil, nil)

	rec := storage.AgentRecord{
		ID:         "agent-bench",
		NodeName:   "node-1",
		Version:    "test",
		LastSeenAt: time.Now().UTC(),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.agents.Enqueue(rec)
		// Drop the buffer every maxSize items to keep the benchmark bounded
		// in memory. We measure Enqueue in isolation; the flush path is
		// covered by BenchmarkBatchWriterFlush.
		if (i+1)%batchSizeFor("agents") == 0 {
			w.agents.mu.Lock()
			w.agents.items = w.agents.items[:0]
			w.agents.mu.Unlock()
			// Drain the signal channel so it does not stay full.
			select {
			case <-w.agents.signal:
			default:
			}
		}
	}
}

// BenchmarkBatchWriterFlush measures one full drain of a saturated agents
// buffer (batchSizeFor("agents") items) through the real SQLite PutAgent
// path. This is the main target for PERF-06 bulk-insert: a bulk path should
// drop this number substantially vs the current row-at-a-time loop. Each op
// corresponds to one full batch; divide by batchSizeFor("agents") to get
// per-row cost.
func BenchmarkBatchWriterFlush(b *testing.B) {
	store, err := sqlite.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("sqlite.Open: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })

	w := New(store, nil, nil)
	w.sleep = func(time.Duration) {} // collapse retry backoff if any row fails

	// Build unique records per iteration so the UNIQUE(id) constraint does
	// not force a retry loop; upserts on the same ID would also collapse
	// into no-op writes and misrepresent the flush cost.
	batchSize := batchSizeFor("agents")
	records := make([]storage.AgentRecord, batchSize)
	for i := range records {
		records[i] = storage.AgentRecord{
			NodeName:   fmt.Sprintf("node-%04d", i),
			Version:    "test",
			LastSeenAt: time.Now().UTC(),
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < batchSize; j++ {
			records[j].ID = fmt.Sprintf("agent-%d-%d", i, j)
			w.agents.Enqueue(records[j])
		}
		w.agents.Drain(context.Background())
	}
}

// BenchmarkBatchWriterMetricsFlush measures the metrics stream, which is the
// highest-volume buffer in production (one snapshot per agent per 15s). PERF-06
// should improve this noticeably because metrics rows are append-only and
// especially amenable to a single multi-row INSERT.
func BenchmarkBatchWriterMetricsFlush(b *testing.B) {
	store, err := sqlite.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("sqlite.Open: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })

	w := New(store, nil, nil)
	w.sleep = func(time.Duration) {}

	batchSize := batchSizeFor("metrics")
	records := make([]storage.MetricSnapshotRecord, batchSize)
	for i := range records {
		records[i] = storage.MetricSnapshotRecord{
			AgentID:    "agent-bench",
			InstanceID: "instance-bench",
			CapturedAt: time.Now().UTC(),
			Values: map[string]uint64{
				"cpu":     42,
				"rss_kib": 123456,
				"conns":   7,
			},
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < batchSize; j++ {
			records[j].ID = fmt.Sprintf("snap-%d-%d", i, j)
			w.metricsBuf.Enqueue(records[j])
		}
		w.metricsBuf.Drain(context.Background())
	}
}

// BenchmarkBatchWriterAuditEnqueue measures the audit-write hot path used on
// every mutating HTTP request. The audit stream must stay O(1) under the hood
// even if the DB stalls (see TestAuditWriteIsAsync), so this number should be
// sub-microsecond and stable across PERF-06.
func BenchmarkBatchWriterAuditEnqueue(b *testing.B) {
	store, err := sqlite.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("sqlite.Open: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })

	w := New(store, nil, nil)

	rec := storage.AuditEventRecord{
		ID:        "audit-bench",
		ActorID:   "user-1",
		Action:    "bench.action",
		TargetID:  "target-1",
		CreatedAt: time.Now().UTC(),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.auditEvents.Enqueue(rec)
		if (i+1)%batchSizeFor("audit") == 0 {
			w.auditEvents.mu.Lock()
			w.auditEvents.items = w.auditEvents.items[:0]
			w.auditEvents.mu.Unlock()
			select {
			case <-w.auditEvents.signal:
			default:
			}
		}
	}
}

// BenchmarkEventHubPublishNoSubscribers measures the lower bound of publish()
// with no subscribers attached — the lock-snapshot overhead only. Useful as a
// regression guard: any change that makes publish() allocate on the
// zero-subscriber path will show up here.
func BenchmarkEventHubPublishNoSubscribers(b *testing.B) {
	hub := eventbus.NewHub()
	evt := eventbus.Event{Type: "jobs.created", Data: map[string]any{"id": "job-1"}}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.Publish(evt)
	}
}

// BenchmarkEventHubPublish100Subscribers measures the realistic steady-state
// cost of an event broadcast when ~100 browser tabs / SSE streams are
// attached. The subscribers drain eagerly so publish() never enters the
// drop-on-full branch.
func BenchmarkEventHubPublish100Subscribers(b *testing.B) {
	hub := eventbus.NewHub()
	const subs = 100

	cancels := make([]func(), 0, subs)
	done := make(chan struct{})
	defer close(done)

	for i := 0; i < subs; i++ {
		ch, cancel := hub.Subscribe()
		cancels = append(cancels, cancel)
		go func(ch <-chan eventbus.Event) {
			for {
				select {
				case <-done:
					return
				case <-ch:
				}
			}
		}(ch)
	}
	b.Cleanup(func() {
		for _, c := range cancels {
			c()
		}
	})

	evt := eventbus.Event{Type: "jobs.created", Data: map[string]any{"id": "job-1"}}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.Publish(evt)
	}
}

// BenchmarkBatchWriterTelemetryDrain2000 models one flush tick of a
// 2000-agent fleet: every agent contributed one TelemetryWriteUnit with a
// runtime blob, 3 DC rows, 2 upstreams and 1 event. Measures the wall
// cost of drainAll against a real on-disk SQLite store (P6, finding #10).
//
// Before/after comparison:
//
//	cp internal/controlplane/server/batch_writer_bench_test.go \
//	   ../../.worktrees/p6-bench-main/internal/controlplane/server/
//	(cd ../../.worktrees/p6-bench-main && go test -run '^$' \
//	   -bench BenchmarkBatchWriterTelemetryDrain2000 -benchtime=5x -count=5 \
//	   ./internal/controlplane/server/) > /tmp/bw-main.txt
//	go test -run '^$' -bench BenchmarkBatchWriterTelemetryDrain2000 \
//	   -benchtime=5x -count=5 ./internal/controlplane/server/ > /tmp/bw-p6.txt
//	benchstat /tmp/bw-main.txt /tmp/bw-p6.txt
func BenchmarkBatchWriterTelemetryDrain2000(b *testing.B) {
	store, err := sqlite.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	defer store.Close()

	const fleet = 2000
	ts := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)

	// Seed the fleet group + agents once: the telemt_* tables carry a FK
	// to agents, so without this every telemetry insert would fail the
	// foreign-key check and the benchmark would time the error path.
	ctx := context.Background()
	if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{ID: "fg-bench", Name: "bench", CreatedAt: ts}); err != nil {
		b.Fatalf("seed fleet group: %v", err)
	}
	agents := make([]storage.AgentRecord, fleet)
	for a := 0; a < fleet; a++ {
		agentID := fmt.Sprintf("agent-%04d", a)
		agents[a] = storage.AgentRecord{ID: agentID, NodeName: agentID, FleetGroupID: "fg-bench", Version: "dev", LastSeenAt: ts}
	}
	if err := store.PutAgentsBulk(ctx, agents); err != nil {
		b.Fatalf("seed agents: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		w := New(store, nil, nil)
		for a := 0; a < fleet; a++ {
			agentID := fmt.Sprintf("agent-%04d", a)
			unit := TelemetryWriteUnit{
				AgentID: agentID,
				Runtime: &storage.TelemetryRuntimeCurrentRecord{
					AgentID: agentID, ObservedAt: ts, RuntimeJSON: `{"state":"ok","cpu":42.5}`,
				},
				DCs: []storage.TelemetryRuntimeDCRecord{
					{AgentID: agentID, DC: 1, ObservedAt: ts, CoveragePct: 100},
					{AgentID: agentID, DC: 2, ObservedAt: ts, CoveragePct: 98},
					{AgentID: agentID, DC: 3, ObservedAt: ts, CoveragePct: 97},
				},
				Upstreams: []storage.TelemetryRuntimeUpstreamRecord{
					{AgentID: agentID, UpstreamID: 1, ObservedAt: ts, Healthy: true},
					{AgentID: agentID, UpstreamID: 2, ObservedAt: ts, Healthy: true},
				},
				Events: []storage.TelemetryRuntimeEventRecord{
					{AgentID: agentID, Sequence: int64(i*fleet + a), ObservedAt: ts, Timestamp: ts, EventType: "tick"},
				},
			}
			w.telemetry.Enqueue(unit)
		}
		b.StartTimer()
		w.drainAll(context.Background())
	}
}
