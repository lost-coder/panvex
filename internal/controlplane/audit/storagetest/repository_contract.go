// internal/controlplane/audit/storagetest/repository_contract.go
//
// RunContract exercises any audit.Repository implementation. Backends
// invoke this from their own *_test.go to verify they meet the contract.
package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/audit"
)

// OpenRepo is a factory that creates a fresh, empty Repository for a single
// sub-test. Each sub-test receives its own instance so state does not leak.
type OpenRepo func(t *testing.T) audit.Repository

// RunContract runs all repository contract sub-tests against the given
// OpenRepo factory. Backends call this once from their *_test.go files.
func RunContract(t *testing.T, open OpenRepo) {
	t.Helper()
	t.Run("AppendRoundTrip", func(t *testing.T) { runAppendRoundTrip(t, open(t)) })
	t.Run("AppendNilDetails", func(t *testing.T) { runAppendNilDetails(t, open(t)) })
	t.Run("AppendMultiple", func(t *testing.T) { runAppendMultiple(t, open(t)) })
}

func runAppendRoundTrip(t *testing.T, repo audit.Repository) {
	t.Helper()
	ctx := context.Background()
	e := audit.Event{
		ID:        "evt-rt-1",
		ActorID:   "user-1",
		Action:    "client.create",
		TargetID:  "client-1",
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		Details:   map[string]any{"key": "value"},
		PrevHash:  "prev-hash",
		EventHash: "event-hash",
	}
	if err := repo.Append(ctx, e); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

func runAppendNilDetails(t *testing.T, repo audit.Repository) {
	t.Helper()
	ctx := context.Background()
	e := audit.Event{
		ID:        "evt-nil-1",
		ActorID:   "user-1",
		Action:    "client.delete",
		TargetID:  "client-2",
		CreatedAt: time.Unix(1700000001, 0).UTC(),
		Details:   nil,
		PrevHash:  "",
		EventHash: "event-hash-2",
	}
	if err := repo.Append(ctx, e); err != nil {
		t.Fatalf("Append with nil details: %v", err)
	}
}

func runAppendMultiple(t *testing.T, repo audit.Repository) {
	t.Helper()
	ctx := context.Background()
	events := []audit.Event{
		{
			ID:        "evt-multi-1",
			ActorID:   "user-1",
			Action:    "client.create",
			TargetID:  "client-3",
			CreatedAt: time.Unix(1700000010, 0).UTC(),
			Details:   map[string]any{"count": 1},
			PrevHash:  "",
			EventHash: "h1",
		},
		{
			ID:        "evt-multi-2",
			ActorID:   "user-2",
			Action:    "client.update",
			TargetID:  "client-3",
			CreatedAt: time.Unix(1700000011, 0).UTC(),
			Details:   map[string]any{"count": 2},
			PrevHash:  "h1",
			EventHash: "h2",
		},
	}
	for _, e := range events {
		if err := repo.Append(ctx, e); err != nil {
			t.Fatalf("Append %s: %v", e.ID, err)
		}
	}
}
