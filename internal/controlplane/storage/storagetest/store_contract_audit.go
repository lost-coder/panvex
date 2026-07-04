package storagetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runAuditContract extracts the audit append+prune contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runAuditContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("audit append and list round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		event := storage.AuditEventRecord{
			ID:        "audit-000001",
			ActorID:   "user-000001",
			Action:    "auth.login",
			TargetID:  "session-000001",
			CreatedAt: time.Date(2026, time.March, 15, 8, 35, 0, 0, time.UTC),
			Details: map[string]any{
				"username": "admin",
			},
		}

		if err := store.AppendAuditEvent(ctx, event); err != nil {
			t.Fatalf("AppendAuditEvent() error = %v", err)
		}

		events, err := store.ListAuditEvents(ctx, 0)
		if err != nil {
			t.Fatalf("ListAuditEvents() error = %v", err)
		}

		if len(events) != 1 {
			t.Fatalf("len(ListAuditEvents()) = %d, want 1", len(events))
		}
	})

	t.Run("audit prune deletes rows older than cutoff", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		baseTime := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)

		seed := []storage.AuditEventRecord{
			{ID: "audit-old-1", ActorID: "u", Action: "a", TargetID: "t", CreatedAt: baseTime.Add(-72 * time.Hour), Details: map[string]any{"k": "1"}},
			{ID: "audit-old-2", ActorID: "u", Action: "a", TargetID: "t", CreatedAt: baseTime.Add(-48 * time.Hour), Details: map[string]any{"k": "2"}},
			{ID: "audit-keep-1", ActorID: "u", Action: "a", TargetID: "t", CreatedAt: baseTime.Add(-12 * time.Hour), Details: map[string]any{"k": "3"}},
			{ID: "audit-keep-2", ActorID: "u", Action: "a", TargetID: "t", CreatedAt: baseTime, Details: map[string]any{"k": "4"}},
		}
		for _, e := range seed {
			if err := store.AppendAuditEvent(ctx, e); err != nil {
				t.Fatalf("AppendAuditEvent(%s) error = %v", e.ID, err)
			}
		}

		cutoff := baseTime.Add(-24 * time.Hour)
		pruned, err := store.PruneAuditEvents(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneAuditEvents() error = %v", err)
		}
		if pruned != 2 {
			t.Fatalf("PruneAuditEvents() pruned = %d, want 2", pruned)
		}

		events, err := store.ListAuditEvents(ctx, 0)
		if err != nil {
			t.Fatalf("ListAuditEvents() error = %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("len(ListAuditEvents()) after prune = %d, want 2", len(events))
		}
		for _, e := range events {
			if e.CreatedAt.Before(cutoff) {
				t.Fatalf("retained event %q has CreatedAt %v before cutoff %v", e.ID, e.CreatedAt, cutoff)
			}
		}

		// A second call with the same cutoff is a no-op.
		pruned2, err := store.PruneAuditEvents(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneAuditEvents(second) error = %v", err)
		}
		if pruned2 != 0 {
			t.Fatalf("PruneAuditEvents(second) pruned = %d, want 0", pruned2)
		}
	})

	t.Run("ListAuditEventsCursor paginates newest-first with stable cursor", func(t *testing.T) {
		// S25 T1 mirror of the jobs cursor contract — same shape, same
		// guarantees: contiguous newest-first pages, no overlap, no gaps.
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		base := time.Date(2026, time.May, 1, 9, 0, 0, 0, time.UTC)
		const total = 9
		want := make([]string, 0, total)
		for i := 0; i < total; i++ {
			id := fmt.Sprintf("audit-%02d", i)
			if err := store.AppendAuditEvent(ctx, storage.AuditEventRecord{
				ID:        id,
				ActorID:   "user-1",
				Action:    "test.cursor",
				TargetID:  "t",
				CreatedAt: base.Add(time.Duration(i) * time.Minute),
				Details:   map[string]any{"i": i},
			}); err != nil {
				t.Fatalf("AppendAuditEvent(%s): %v", id, err)
			}
			want = append([]string{id}, want...)
		}

		got := make([]string, 0, total)
		params := storage.ListAuditEventsCursorParams{Limit: 4}
		for page := 0; page < 5; page++ {
			rows, next, err := store.ListAuditEventsCursor(ctx, params)
			if err != nil {
				t.Fatalf("ListAuditEventsCursor page %d: %v", page, err)
			}
			for _, row := range rows {
				got = append(got, row.ID)
			}
			if next.AfterID == "" {
				break
			}
			params = next
		}
		if len(got) != total {
			t.Fatalf("walked %d audit events across pages, want %d", len(got), total)
		}
		for i, id := range want {
			if got[i] != id {
				t.Fatalf("page-walk[%d] = %q, want %q (sequence: %v)", i, got[i], id, got)
			}
		}
	})

	// P2-REL-05: metric_snapshots must be prunable by captured_at cutoff.

}
