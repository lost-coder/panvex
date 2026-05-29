package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/audit/hashchain"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestAuditChainSurvivesStorageRoundTrip is the regression guard for the
// timestamp-precision defect (C-1). The producer hashes CreatedAt via
// RFC3339Nano, but the at-rest store persists whole Unix seconds (toUnix).
// If the producer does not truncate CreatedAt to the second before hashing,
// the verifier recomputes a different event_hash from the read-back
// (seconds-precision) row and reports false tampering on every event,
// making verify-audit-chain unusable.
//
// We append events with a nanosecond-bearing clock (like time.Now() in
// production), read the rows back from SQLite, and recompute the chain
// exactly as cmd/control-plane verify-audit-chain does.
func TestAuditChainSurvivesStorageRoundTrip(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// A clock carrying sub-second nanoseconds, mirroring real time.Now().
	clock := time.Unix(1700000000, 738291042).UTC()
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return clock },
		Store:            store,
	})
	t.Cleanup(func() { srv.Close() })

	const n = 3
	for i := 0; i < n; i++ {
		if err := srv.appendAuditSync(context.Background(), "user-1", "test.action", "target-1", map[string]any{"i": i}); err != nil {
			t.Fatalf("appendAuditSync: %v", err)
		}
	}

	rows, err := store.ListAuditEvents(context.Background(), n+5)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(rows) != n {
		t.Fatalf("got %d audit rows, want %d", len(rows), n)
	}

	// Recompute the chain exactly like the verifier: each row's stored
	// event_hash must equal ComputeEventHash(prev, row) over the row as
	// read back from storage.
	prev := ""
	for i, row := range rows {
		if row.PrevHash != prev {
			t.Fatalf("row %d prev_hash mismatch: stored %q want %q", i, row.PrevHash, prev)
		}
		want, err := hashchain.ComputeEventHash(prev, row)
		if err != nil {
			t.Fatalf("ComputeEventHash: %v", err)
		}
		if row.EventHash != want {
			t.Fatalf("row %d event_hash mismatch after storage round-trip:\n stored:   %s\n computed: %s\n(producer must Truncate CreatedAt to the second)", i, row.EventHash, want)
		}
		prev = row.EventHash
	}
}
