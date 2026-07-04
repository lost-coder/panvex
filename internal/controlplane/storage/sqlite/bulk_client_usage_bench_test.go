package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// openBenchStore mirrors openTestStore (pragmas_test.go) but takes *testing.B.
func openBenchStore(b *testing.B) (*Store, func()) {
	b.Helper()
	path := filepath.Join(b.TempDir(), "bench.db")
	store, err := Open(path)
	if err != nil {
		b.Fatalf("Open() error = %v", err)
	}
	return store, func() { _ = store.Close() }
}

// BenchmarkUpsertClientUsage_Loop / _Bulk compare the per-tick persist cost
// of the legacy loop-of-singles versus the bulk transaction added in P-1
// (sprint S-23 perf-critical). The fixture is shaped after the audit
// scenario: one tick worth of (client, agent) rows.
//
// Run: go test -bench=BenchmarkUpsertClientUsage -benchmem ./internal/controlplane/storage/sqlite/

// benchClientUsageRows is sized so a single iteration completes quickly
// during routine benchmark runs while still being big enough to make the
// per-Exec overhead visible. The audit P-1 scenario worst case is 25k rows
// per persist call (500 clients x 50 agents); raise this constant locally
// to reproduce that shape.
const benchClientUsageRows = 500

func benchSetupClientUsage(b *testing.B) (*Store, []storage.ClientUsageRecord) {
	b.Helper()
	store, cleanup := openBenchStore(b)
	b.Cleanup(cleanup)

	ctx := context.Background()
	now := time.Now().UTC()
	group := storage.FleetGroupRecord{ID: "bench-grp", Name: "Bench", CreatedAt: now}
	if err := store.PutFleetGroup(ctx, group); err != nil {
		b.Fatalf("PutFleetGroup: %v", err)
	}
	agent := storage.AgentRecord{
		ID: "bench-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: now,
	}
	if err := store.PutAgent(ctx, agent); err != nil {
		b.Fatalf("PutAgent: %v", err)
	}
	records := make([]storage.ClientUsageRecord, 0, benchClientUsageRows)
	for i := 0; i < benchClientUsageRows; i++ {
		clientID := fmt.Sprintf("c-%04d", i)
		client := storage.ClientRecord{
			ID: clientID, Name: clientID, SecretCiphertext: "s",
			UserADTag: "0123456789abcdef0123456789abcdef",
			Enabled:   true, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.PutClient(ctx, client); err != nil {
			b.Fatalf("PutClient: %v", err)
		}
		records = append(records, storage.ClientUsageRecord{
			ClientID: clientID, AgentID: agent.ID,
			TrafficUsedBytes: uint64(1000 + i), UniqueIPsUsed: 2,
			ActiveTCPConns: 1, ActiveUniqueIPs: 1,
			AgentBootID: "boot-bench", LastTotalBytes: uint64(i + 1), ObservedAt: now,
		})
	}
	return store, records
}

func BenchmarkUpsertClientUsage_Loop(b *testing.B) {
	store, records := benchSetupClientUsage(b)
	ctx := context.Background()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for _, r := range records {
			if err := store.UpsertClientUsage(ctx, r); err != nil {
				b.Fatalf("UpsertClientUsage: %v", err)
			}
		}
	}
}

func BenchmarkUpsertClientUsage_Bulk(b *testing.B) {
	store, records := benchSetupClientUsage(b)
	ctx := context.Background()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if err := store.UpsertClientUsageBulk(ctx, records); err != nil {
			b.Fatalf("UpsertClientUsageBulk: %v", err)
		}
	}
}
