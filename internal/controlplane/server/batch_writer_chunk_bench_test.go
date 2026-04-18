package server

// P3-PERF-01b: chunk-size sweep for the bulk-insert path. This bench exists
// only to answer "is bulkChunkSize = 500 the right knob?" and is intentionally
// a sub-test-style loop rather than a set of separate Benchmark* functions.
//
// It varies the number of rows handed to PutAgentsBulk / AppendMetricSnapshotsBulk
// in one call. bulkChunkSize (500) is the *upper* bound used to split large
// buffers into multiple INSERTs; batchMaxSize (50) is the *actual* size that
// the control-plane batch writer hands over in production. Sweeping both sides
// of 500 tells us whether the constant should be lowered (less SQL string
// build cost, fewer bind params) or raised (fewer statements per buffer drain).
//
// NOTE: we bypass the storeBatchWriter layer and call the Store bulk method
// directly so we isolate the INSERT cost from the retry/classify/metrics
// wrappers. Each sub-benchmark opens its own SQLite tempdir so writes do not
// compound across iterations.

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

func BenchmarkBulkAgentsChunkSweep(b *testing.B) {
	sizes := []int{50, 100, 250, 500, 1000, 2000}
	for _, n := range sizes {
		n := n
		b.Run(fmt.Sprintf("rows=%d", n), func(b *testing.B) {
			store, err := sqlite.Open(filepath.Join(b.TempDir(), "bench.db"))
			if err != nil {
				b.Fatalf("sqlite.Open: %v", err)
			}
			b.Cleanup(func() { _ = store.Close() })

			records := make([]storage.AgentRecord, n)
			for i := range records {
				records[i] = storage.AgentRecord{
					NodeName:   fmt.Sprintf("node-%04d", i),
					Version:    "test",
					LastSeenAt: time.Now().UTC(),
				}
			}

			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := range records {
					records[j].ID = fmt.Sprintf("agent-%d-%d", i, j)
				}
				if err := store.PutAgentsBulk(ctx, records); err != nil {
					b.Fatalf("PutAgentsBulk(n=%d): %v", n, err)
				}
			}
			// Per-row cost is ns/op divided by n — use b.ReportMetric so it
			// appears in the output alongside the standard row.
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/row")
		})
	}
}

func BenchmarkBulkMetricsChunkSweep(b *testing.B) {
	sizes := []int{50, 100, 250, 500, 1000, 2000}
	for _, n := range sizes {
		n := n
		b.Run(fmt.Sprintf("rows=%d", n), func(b *testing.B) {
			store, err := sqlite.Open(filepath.Join(b.TempDir(), "bench.db"))
			if err != nil {
				b.Fatalf("sqlite.Open: %v", err)
			}
			b.Cleanup(func() { _ = store.Close() })

			// Seed agent + instance the metric snapshots FK-reference, matching
			// what BenchmarkBatchWriterMetricsFlush does implicitly via the
			// batch writer. Without this the bulk INSERT fails on a FK.
			ctxSeed := context.Background()
			if err := store.PutAgent(ctxSeed, storage.AgentRecord{
				ID: "agent-bench", NodeName: "node-1", Version: "test",
				LastSeenAt: time.Now().UTC(),
			}); err != nil {
				b.Fatalf("seed agent: %v", err)
			}
			if err := store.PutInstance(ctxSeed, storage.InstanceRecord{
				ID: "instance-bench", AgentID: "agent-bench", Name: "inst", Version: "test",
				UpdatedAt: time.Now().UTC(),
			}); err != nil {
				b.Fatalf("seed instance: %v", err)
			}

			records := make([]storage.MetricSnapshotRecord, n)
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

			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := range records {
					records[j].ID = fmt.Sprintf("snap-%d-%d", i, j)
				}
				if err := store.AppendMetricSnapshotsBulk(ctx, records); err != nil {
					b.Fatalf("AppendMetricSnapshotsBulk(n=%d): %v", n, err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/row")
		})
	}
}
