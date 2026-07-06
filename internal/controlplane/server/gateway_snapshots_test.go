package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
)

// TestConvertClientUsageSnapshotsCarriesQuotaState moved to the gateway
// package with convertClientUsageSnapshots (P8.2d); see
// internal/controlplane/gateway/gateway_snapshots_test.go.

// TestMergeClientUsageBatchRetainsQuotaState ensures the in-memory
// clientUsage mirror keeps the latest quota state across merges so the
// HTTP layer (and any subsequent reset action in Phase 2) reads a
// fresh value. Last-write-wins — the latest snapshot's quota fields
// replace the prior ones, just like the live gauges.
func TestMergeClientUsageBatchRetainsQuotaState(t *testing.T) {
	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	defer server.Close()

	const agentID = "agent-merge-quota"
	const clientID = "client-merge-quota"
	seedClientAndAgentRows(t, server, clientID, agentID, now)

	first := []clients.UsageReport{{
		ClientID:           clientID,
		TotalBytes:         100,
		QuotaUsedBytes:     500,
		QuotaLastResetUnix: 1_715_000_000,
		ObservedAt:         now,
	}}
	second := []clients.UsageReport{{
		ClientID:           clientID,
		TotalBytes:         200,
		QuotaUsedBytes:     750,
		QuotaLastResetUnix: 1_715_000_100,
		ObservedAt:         now.Add(time.Minute),
	}}

	server.mu.Lock()
	server.applyClientUsageSnapshot(t.Context(), agentID, "boot-1", first)
	server.applyClientUsageSnapshot(t.Context(), agentID, "boot-1", second)
	got := mirrorUsage(server, clientID, agentID)
	server.mu.Unlock()

	if got.QuotaUsedBytes != 750 {
		t.Errorf("QuotaUsedBytes = %d, want 750 (latest snapshot wins)", got.QuotaUsedBytes)
	}
	if got.QuotaLastResetUnix != 1_715_000_100 {
		t.Errorf("QuotaLastResetUnix = %d, want 1715000100 (latest snapshot wins)", got.QuotaLastResetUnix)
	}
}

// TestUsageSnapshotJSONShapeHasQuotaFields guards the wire-contract
// the frontend zod schema agrees on: the JSON field names for the new
// fields are exactly "quota_used_bytes" and "quota_last_reset_unix".
// A rename here would silently break the dashboard.
func TestUsageSnapshotJSONShapeHasQuotaFields(t *testing.T) {
	snap := clients.UsageSnapshot{
		ClientID:           "c-1",
		TrafficUsedBytes:   42,
		UniqueIPsUsed:      1,
		ActiveTCPConns:     2,
		ActiveUniqueIPs:    3,
		QuotaUsedBytes:     1024,
		QuotaLastResetUnix: 1_715_000_000,
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal UsageSnapshot: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal UsageSnapshot: %v", err)
	}

	used, ok := decoded["quota_used_bytes"]
	if !ok {
		t.Fatalf("json key \"quota_used_bytes\" missing; got keys: %v", keysOf(decoded))
	}
	if got, _ := used.(float64); got != 1024 {
		t.Errorf("quota_used_bytes = %v, want 1024", used)
	}

	reset, ok := decoded["quota_last_reset_unix"]
	if !ok {
		t.Fatalf("json key \"quota_last_reset_unix\" missing; got keys: %v", keysOf(decoded))
	}
	if got, _ := reset.(float64); got != 1_715_000_000 {
		t.Errorf("quota_last_reset_unix = %v, want 1715000000", reset)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
