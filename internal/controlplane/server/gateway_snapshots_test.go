package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestConvertClientUsageSnapshotsCarriesQuotaState guards Phase 1 of the
// reset-quota plan: the panel's mapping of the gRPC wire
// ClientUsageSnapshot into the in-memory clientUsageSnapshot mirror must
// pass through the two new quota fields (QuotaUsedBytes,
// QuotaLastResetUnix) verbatim. Without this the panel JSON API can
// never surface the per-client quota state the agent now reports.
func TestConvertClientUsageSnapshotsCarriesQuotaState(t *testing.T) {
	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	defer server.Close()

	const agentID = "agent-quota"
	const clientID = "client-quota"

	wire := []*gatewayrpc.ClientUsageSnapshot{{
		ClientId:           clientID,
		TrafficDeltaBytes:  2048,
		UniqueIpsUsed:      3,
		ActiveTcpConns:     4,
		ActiveUniqueIps:    5,
		Seq:                7,
		QuotaUsedBytes:     1_234_567,
		QuotaLastResetUnix: 1_715_000_000,
	}}

	out, resolved, skipped := server.convertClientUsageSnapshots(agentID, wire, now)
	if resolved != 1 || skipped != 0 {
		t.Fatalf("resolve counts = (%d, %d), want (1, 0)", resolved, skipped)
	}
	if len(out) != 1 {
		t.Fatalf("converted slice len = %d, want 1", len(out))
	}
	got := out[0]
	if got.QuotaUsedBytes != 1_234_567 {
		t.Errorf("QuotaUsedBytes = %d, want 1234567 (proto -> panel mirror must be pass-through)", got.QuotaUsedBytes)
	}
	if got.QuotaLastResetUnix != 1_715_000_000 {
		t.Errorf("QuotaLastResetUnix = %d, want 1715000000 (proto -> panel mirror must be pass-through)", got.QuotaLastResetUnix)
	}
}

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

	first := []clientUsageSnapshot{{
		ClientID:           clientID,
		TrafficUsedBytes:   100,
		QuotaUsedBytes:     500,
		QuotaLastResetUnix: 1_715_000_000,
		ObservedAt:         now,
		Seq:                1,
	}}
	second := []clientUsageSnapshot{{
		ClientID:           clientID,
		TrafficUsedBytes:   200,
		QuotaUsedBytes:     750,
		QuotaLastResetUnix: 1_715_000_100,
		ObservedAt:         now.Add(time.Minute),
		Seq:                2,
	}}

	server.mu.Lock()
	server.applyClientUsageSnapshot(t.Context(), agentID, first)
	server.applyClientUsageSnapshot(t.Context(), agentID, second)
	got := server.clientUsage[clientID][agentID]
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
		Seq:                9,
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
