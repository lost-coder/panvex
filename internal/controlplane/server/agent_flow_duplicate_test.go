package server

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// TestAuditDuplicateNodeName (R9b Task 6): a freshly-enrolled agent sharing a
// node_name with an existing agent must surface a Warn (and an audit event) so
// the operator can spot a cloned VM / re-run bootstrap; a unique node_name
// must not.
func TestAuditDuplicateNodeName(t *testing.T) {
	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	h := &recordingHandler{}
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Logger:           slog.New(h),
	})
	defer server.Close()

	const msg = "enrolled agent shares node_name with existing agent(s) — possible duplicate identity (cloned VM / re-run bootstrap)"
	countWarn := func() int {
		h.mu.Lock()
		defer h.mu.Unlock()
		n := 0
		for _, r := range h.records {
			if r.Message == msg && r.Level == slog.LevelWarn {
				n++
			}
		}
		return n
	}

	// Existing agent + the freshly-enrolled one both carry node_name "dup".
	server.live.ApplySnapshot("agent-old", Agent{ID: "agent-old", NodeName: "dup"}, nil)
	server.live.ApplySnapshot("agent-new", Agent{ID: "agent-new", NodeName: "dup"}, nil)

	server.auditDuplicateNodeName(context.Background(), "agent-new", "dup")
	if got := countWarn(); got != 1 {
		t.Fatalf("duplicate node_name warns = %d, want 1", got)
	}

	// A unique node_name must not warn.
	server.live.ApplySnapshot("agent-uniq", Agent{ID: "agent-uniq", NodeName: "solo"}, nil)
	server.auditDuplicateNodeName(context.Background(), "agent-uniq", "solo")
	if got := countWarn(); got != 1 {
		t.Fatalf("warns after unique enrollment = %d, want still 1", got)
	}
}
