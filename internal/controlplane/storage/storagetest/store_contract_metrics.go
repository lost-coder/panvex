package storagetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runMetricsContract extracts the metric append+prune contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runMetricsContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("metric append and list round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		// P2-DB-03: metric_snapshots.agent_id now has ON DELETE CASCADE
		// FK to agents(id); the referenced agent must exist.
		if err := store.PutAgent(ctx, storage.AgentRecord{
			ID:         "agent-000001",
			NodeName:   "node-metric",
			LastSeenAt: time.Date(2026, time.March, 15, 8, 40, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}
		snapshot := storage.MetricSnapshotRecord{
			ID:         "metric-000001",
			AgentID:    "agent-000001",
			InstanceID: "instance-000001",
			CapturedAt: time.Date(2026, time.March, 15, 8, 40, 0, 0, time.UTC),
			Values: map[string]uint64{
				"connections": 42,
			},
		}

		if err := store.AppendMetricSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("AppendMetricSnapshot() error = %v", err)
		}

		snapshots, err := store.ListMetricSnapshots(ctx)
		if err != nil {
			t.Fatalf("ListMetricSnapshots() error = %v", err)
		}

		if len(snapshots) != 1 {
			t.Fatalf("len(ListMetricSnapshots()) = %d, want 1", len(snapshots))
		}
	})

	// P2-REL-04 / finding M-R2: audit_events must be prunable by cutoff so
	// the retention worker can bound table growth.

	t.Run("metric snapshot prune deletes rows older than cutoff", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		baseTime := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)

		// P2-DB-03: metric_snapshots.agent_id has a CASCADE FK — seed the
		// agent so the inserts do not trip the constraint.
		if err := store.PutAgent(ctx, storage.AgentRecord{
			ID:         "a1",
			NodeName:   "node-prune",
			LastSeenAt: baseTime,
		}); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}

		seed := []storage.MetricSnapshotRecord{
			{ID: "metric-old-1", AgentID: "a1", InstanceID: "i1", CapturedAt: baseTime.Add(-72 * time.Hour), Values: map[string]uint64{"x": 1}},
			{ID: "metric-old-2", AgentID: "a1", InstanceID: "i1", CapturedAt: baseTime.Add(-48 * time.Hour), Values: map[string]uint64{"x": 2}},
			{ID: "metric-keep-1", AgentID: "a1", InstanceID: "i1", CapturedAt: baseTime.Add(-12 * time.Hour), Values: map[string]uint64{"x": 3}},
			{ID: "metric-keep-2", AgentID: "a1", InstanceID: "i1", CapturedAt: baseTime, Values: map[string]uint64{"x": 4}},
		}
		for _, m := range seed {
			if err := store.AppendMetricSnapshot(ctx, m); err != nil {
				t.Fatalf("AppendMetricSnapshot(%s) error = %v", m.ID, err)
			}
		}

		cutoff := baseTime.Add(-24 * time.Hour)
		pruned, err := store.PruneMetricSnapshots(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneMetricSnapshots() error = %v", err)
		}
		if pruned != 2 {
			t.Fatalf("PruneMetricSnapshots() pruned = %d, want 2", pruned)
		}

		snapshots, err := store.ListMetricSnapshots(ctx)
		if err != nil {
			t.Fatalf("ListMetricSnapshots() error = %v", err)
		}
		if len(snapshots) != 2 {
			t.Fatalf("len(ListMetricSnapshots()) after prune = %d, want 2", len(snapshots))
		}
		for _, m := range snapshots {
			if m.CapturedAt.Before(cutoff) {
				t.Fatalf("retained snapshot %q has CapturedAt %v before cutoff %v", m.ID, m.CapturedAt, cutoff)
			}
		}

		// A second call with the same cutoff is a no-op.
		pruned2, err := store.PruneMetricSnapshots(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneMetricSnapshots(second) error = %v", err)
		}
		if pruned2 != 0 {
			t.Fatalf("PruneMetricSnapshots(second) pruned = %d, want 0", pruned2)
		}
	})

	// M4: ListMetricSnapshots must cap at 512 rows on every backend, keeping
	// the NEWEST 512 (oldest→newest in the returned slice). The SQLite and
	// Postgres Store implementations enforce this with an inner
	// "ORDER BY captured_at DESC, id DESC LIMIT 512" subquery re-sorted
	// ascending outside; this contract test guards that both backends (and
	// the dbsqlc-backed query, if ever wired in) honor the same cap.
	t.Run("ListMetricSnapshots caps at 512 keeping newest", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		baseTime := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

		if err := store.PutAgent(ctx, storage.AgentRecord{
			ID:         "agent-cap-000001",
			NodeName:   "node-cap",
			LastSeenAt: baseTime,
		}); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}

		const total = 600
		const want = 512
		batch := make([]storage.MetricSnapshotRecord, 0, total)
		for i := 0; i < total; i++ {
			batch = append(batch, storage.MetricSnapshotRecord{
				ID:         fmt.Sprintf("metric-cap-%06d", i),
				AgentID:    "agent-cap-000001",
				InstanceID: "instance-cap-000001",
				CapturedAt: baseTime.Add(time.Duration(i) * time.Second),
				Values:     map[string]uint64{"seq": uint64(i)},
			})
		}
		if err := store.AppendMetricSnapshotsBulk(ctx, batch); err != nil {
			t.Fatalf("AppendMetricSnapshotsBulk() error = %v", err)
		}

		got, err := store.ListMetricSnapshots(ctx)
		if err != nil {
			t.Fatalf("ListMetricSnapshots() error = %v", err)
		}
		if len(got) != want {
			t.Fatalf("len(ListMetricSnapshots()) = %d, want %d", len(got), want)
		}

		// The kept rows must be the newest `want` of the `total` inserted
		// (indices total-want .. total-1), returned oldest→newest.
		wantFirstIdx := total - want
		for offset, snapshot := range got {
			wantID := fmt.Sprintf("metric-cap-%06d", wantFirstIdx+offset)
			if snapshot.ID != wantID {
				t.Fatalf("got[%d].ID = %q, want %q (oldest-kept must be index %d, newest→ascending order)", offset, snapshot.ID, wantID, wantFirstIdx)
			}
		}
		wantOldestKept := baseTime.Add(time.Duration(wantFirstIdx) * time.Second)
		if !got[0].CapturedAt.Equal(wantOldestKept) {
			t.Fatalf("got[0].CapturedAt = %v, want %v (oldest kept of newest %d)", got[0].CapturedAt, wantOldestKept, want)
		}
		if got[0].CapturedAt.After(got[len(got)-1].CapturedAt) {
			t.Fatalf("ListMetricSnapshots() not ordered oldest→newest: got[0]=%v after got[last]=%v", got[0].CapturedAt, got[len(got)-1].CapturedAt)
		}
	})
}
