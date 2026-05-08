package main

import (
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/audit/hashchain"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func TestVerifyAuditChainRows_HappyChain(t *testing.T) {
	rows := buildChainedRows(t, 5)
	report, mismatch := verifyAuditChainRows(rows)
	if mismatch != nil {
		t.Fatalf("expected consistent chain, got %v\n%s", mismatch, report)
	}
	if !strings.Contains(report, "consistent") {
		t.Fatalf("happy report should mention consistency: %s", report)
	}
}

func TestVerifyAuditChainRows_GenesisPrefixTolerated(t *testing.T) {
	// Three legacy rows (empty hashes from before migration 0038) followed
	// by three real chained rows.
	legacy := []storage.AuditEventRecord{
		{ID: "old1", ActorID: "u", Action: "a", TargetID: "t",
			CreatedAt: time.Unix(1700000000, 0).UTC(),
			Details:   map[string]any{}},
		{ID: "old2", ActorID: "u", Action: "a", TargetID: "t",
			CreatedAt: time.Unix(1700000001, 0).UTC(),
			Details:   map[string]any{}},
	}
	real := buildChainedRows(t, 3)
	rows := make([]storage.AuditEventRecord, 0, len(legacy)+len(real))
	rows = append(rows, legacy...)
	rows = append(rows, real...)

	report, mismatch := verifyAuditChainRows(rows)
	if mismatch != nil {
		t.Fatalf("expected consistent chain after legacy prefix, got %v\n%s", mismatch, report)
	}
	if !strings.Contains(report, "genesis prefix: 2") {
		t.Fatalf("expected genesis prefix line for 2 rows, got: %s", report)
	}
}

func TestVerifyAuditChainRows_DetectsTamperedDetails(t *testing.T) {
	rows := buildChainedRows(t, 4)
	// Rewrite row 2's details after persistence — the EventHash on row
	// 2 still reflects the original payload, so verification must catch
	// the mismatch.
	rows[2].Details = map[string]any{"injected": "evil"}

	report, mismatch := verifyAuditChainRows(rows)
	if mismatch == nil {
		t.Fatalf("expected mismatch, got clean report:\n%s", report)
	}
	if !strings.Contains(report, "BROKEN at event "+rows[2].ID) {
		t.Fatalf("report did not name the offending event: %s", report)
	}
	if !strings.Contains(report, "event_hash mismatch") {
		t.Fatalf("expected event_hash mismatch label, got: %s", report)
	}
}

func TestVerifyAuditChainRows_DetectsBrokenLink(t *testing.T) {
	rows := buildChainedRows(t, 4)
	// Snip the chain: rewrite row 2's prev_hash so it no longer points
	// at row 1's event_hash.
	rows[2].PrevHash = "0000000000000000000000000000000000000000000000000000000000000000"

	report, mismatch := verifyAuditChainRows(rows)
	if mismatch == nil {
		t.Fatalf("expected prev_hash mismatch, got clean report:\n%s", report)
	}
	if !strings.Contains(report, "prev_hash mismatch") {
		t.Fatalf("expected prev_hash mismatch label, got: %s", report)
	}
}

// buildChainedRows constructs n records whose PrevHash/EventHash form a
// valid chain rooted at empty prev. Mirrors what the producer side
// would persist; the helper exists so tests can drive the verifier
// without booting the server.
func buildChainedRows(t *testing.T, n int) []storage.AuditEventRecord {
	t.Helper()
	rows := make([]storage.AuditEventRecord, 0, n)
	prev := ""
	base := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		r := storage.AuditEventRecord{
			ID:        idFor(i),
			ActorID:   "user_admin",
			Action:    "test.event",
			TargetID:  idFor(i + 100),
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			Details:   map[string]any{"step": i, "label": "row"},
			PrevHash:  prev,
		}
		hash, err := hashchain.ComputeEventHash(prev, r)
		if err != nil {
			t.Fatalf("compute: %v", err)
		}
		r.EventHash = hash
		rows = append(rows, r)
		prev = hash
	}
	return rows
}

func idFor(i int) string {
	return "audit-" + intToString(i)
}

func intToString(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
