// Package loadtest is the perf-harness the audit (§3.6) asks for: a
// small, dependency-free set of Go benchmarks that exercise the four
// hottest CP paths so a perf regression turns up in `go test -bench`
// instead of a customer ticket.
//
// Run all benchmarks (SQLite-backed, no external deps required):
//
//	go test -bench . -benchmem -run '^$' ./internal/loadtest/...
//
// Add the Postgres migration scenario by exporting:
//
//	PANVEX_POSTGRES_TEST_DSN=postgres://... go test -bench BenchmarkMigratePostgres ./internal/loadtest/...
//
// Each benchmark deliberately sizes its workload at the lower end of
// production-realistic — enough to surface allocation regressions and
// O(N²) behaviour, fast enough to run in CI.
package loadtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/postgres"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// openSQLite opens a fresh SQLite store under b.TempDir(). Migrations
// run as part of Open, so the bench measures only the steady-state
// path it cares about.
func openSQLite(b *testing.B) *sqlite.Store {
	b.Helper()
	store, err := sqlite.Open(filepath.Join(b.TempDir(), "loadtest.db"))
	if err != nil {
		b.Fatalf("sqlite.Open: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })
	return store
}

// BenchmarkClientsPutSequential measures cold-path PutClient latency
// over N rows. Production hits this path during an operator
// "import N clients" workflow; the audit pegs the throughput target at
// >50 clients/s on commodity SSD.
func BenchmarkClientsPutSequential(b *testing.B) {
	store := openSQLite(b)
	ctx := b.Context()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := store.PutClient(ctx, storage.ClientRecord{
			ID:               "client-" + strconv.Itoa(i),
			Name:             "load-" + strconv.Itoa(i),
			SecretCiphertext: "ciphertext-placeholder",
			UserADTag:        "deadbeefdeadbeefdeadbeefdeadbeef",
			Enabled:          true,
			MaxTCPConns:      4,
			MaxUniqueIPs:     2,
			DataQuotaBytes:   1 << 20,
		})
		if err != nil {
			b.Fatalf("PutClient[%d]: %v", i, err)
		}
	}
}

// BenchmarkTelemetryEventsBulk measures the bulk insert path the
// agent uses every poll cycle (~15 s). 250 events per call mirrors
// bulkChunkSize in the storage layer.
func BenchmarkTelemetryEventsBulk(b *testing.B) {
	store := openSQLite(b)
	// Seed the agents table so the FK on telemt_runtime_events is
	// satisfied; the FK existed before this bench so we're not
	// gaming the workload.
	ctx := b.Context()
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:       "loadtest-agent",
		NodeName: "loadtest",
		Version:  "bench",
	}); err != nil {
		b.Fatalf("PutAgent: %v", err)
	}

	const batch = 250
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		records := make([]storage.TelemetryRuntimeEventRecord, batch)
		for j := range records {
			records[j] = storage.TelemetryRuntimeEventRecord{
				AgentID:    "loadtest-agent",
				Sequence:   int64(i*batch + j),
				ObservedAt: now,
				Timestamp:  now,
				EventType:  "bench.event",
				Context:    "{}",
				Severity:   "info",
			}
		}
		if err := store.AppendTelemetryRuntimeEvents(ctx, "loadtest-agent", records); err != nil {
			b.Fatalf("AppendTelemetryRuntimeEvents[%d]: %v", i, err)
		}
	}
}

// BenchmarkEventBusFanout measures pub→consume throughput on the
// in-process Hub for a fan-out of 8 subscribers. The WS server uses
// the same backend; this is the upper bound for how fast a realtime
// event reaches every connected dashboard.
func BenchmarkEventBusFanout(b *testing.B) {
	hub := eventbus.NewHub()
	const subscribers = 8

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < subscribers; i++ {
		ch, cancel := hub.Subscribe()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			for {
				select {
				case <-stop:
					return
				case _, ok := <-ch:
					if !ok {
						return
					}
				}
			}
		}()
	}

	evt := eventbus.Event{Type: "bench", Data: map[string]any{"i": 0}}
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		hub.Publish(evt)
	}

	b.StopTimer()
	close(stop)
	wg.Wait()
}

// BenchmarkMigrateSQLite measures full goose Up over an empty SQLite
// database. Useful as a regression guard after a new migration lands —
// the per-run cost should rise roughly linearly, not jump by an order
// of magnitude.
func BenchmarkMigrateSQLite(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		path := filepath.Join(b.TempDir(), fmt.Sprintf("migrate-%d.db", i))
		store, err := sqlite.Open(path)
		if err != nil {
			b.Fatalf("sqlite.Open[%d]: %v", i, err)
		}
		_ = store.Close()
	}
}

// BenchmarkMigratePostgres exercises the same path against a real
// Postgres instance. Skipped without PANVEX_POSTGRES_TEST_DSN so
// `go test -bench .` stays runnable without docker. CI should set
// the env to surface migration regressions on the production engine.
func BenchmarkMigratePostgres(b *testing.B) {
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		b.Skip("PANVEX_POSTGRES_TEST_DSN not set")
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Postgres test DSN points at an empty schema; Open runs
		// goose Up. The cost we measure is the migration only.
		ctx := context.Background()
		store, err := postgres.OpenContext(ctx, dsn)
		if err != nil {
			b.Fatalf("postgres.Open[%d]: %v", i, err)
		}
		_ = store.Close()
	}
}
