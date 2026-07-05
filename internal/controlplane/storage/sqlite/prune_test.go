package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func TestPruneChunkedDeletesEverythingOldKeepsNew(t *testing.T) {
	// Уменьшить чанк, чтобы 10 старых строк потребовали 4 итерации.
	old := pruneChunkSize
	pruneChunkSize = 3
	t.Cleanup(func() { pruneChunkSize = old })

	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "prune.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	// metric_snapshots has an FK to agents — seed the fleet group + agent.
	fgAt := time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC)
	if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{ID: "fg-1", Name: "Default", CreatedAt: fgAt}); err != nil {
		t.Fatalf("PutFleetGroup: %v", err)
	}
	if err := store.PutAgent(ctx, storage.AgentRecord{ID: "a1", NodeName: "a1", FleetGroupID: "fg-1", Version: "dev", LastSeenAt: fgAt}); err != nil {
		t.Fatalf("PutAgent: %v", err)
	}

	cutoff := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	// 10 строк старше cutoff + 5 новее. Чанк-механика общая для всех шести
	// таблиц; per-таблицу колонки покрывает существующий контракт Prune*.
	var snapshots []storage.MetricSnapshotRecord
	for i := 0; i < 10; i++ {
		snapshots = append(snapshots, storage.MetricSnapshotRecord{
			ID: fmt.Sprintf("snap-old-%d", i), AgentID: "a1",
			CapturedAt: cutoff.Add(-time.Duration(i+1) * time.Minute),
			Values:     map[string]uint64{"v": uint64(i)},
		})
	}
	for i := 0; i < 5; i++ {
		snapshots = append(snapshots, storage.MetricSnapshotRecord{
			ID: fmt.Sprintf("snap-new-%d", i), AgentID: "a1",
			CapturedAt: cutoff.Add(time.Duration(i+1) * time.Minute),
			Values:     map[string]uint64{"v": uint64(i)},
		})
	}
	if err := store.AppendMetricSnapshotsBulk(ctx, snapshots); err != nil {
		t.Fatalf("seed: %v", err)
	}

	deleted, err := store.PruneMetricSnapshots(ctx, cutoff)
	if err != nil {
		t.Fatalf("PruneMetricSnapshots: %v", err)
	}
	if deleted != 10 {
		t.Fatalf("deleted = %d, want 10 (ALL old rows across 4 chunks)", deleted)
	}
	remaining, err := store.ListMetricSnapshots(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(remaining) != 5 {
		t.Fatalf("remaining = %d, want 5 (new rows untouched)", len(remaining))
	}
}
