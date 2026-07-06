package gateway

import (
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestConvertClientUsageSnapshotsCarriesQuotaState guards Phase 1 of the
// reset-quota plan: the panel's mapping of the gRPC wire
// ClientUsageSnapshot into the inbound clients.UsageReport must
// pass through the two new quota fields (QuotaUsedBytes,
// QuotaLastResetUnix) verbatim. Without this the panel JSON API can
// never surface the per-client quota state the agent now reports.
func TestConvertClientUsageSnapshotsCarriesQuotaState(t *testing.T) {
	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	g := &Gateway{deps: stubDeps{}}

	const clientID = "client-quota"

	wire := []*gatewayrpc.ClientUsageSnapshot{{
		ClientId:           clientID,
		TrafficTotalBytes:  2048,
		UniqueIpsUsed:      3,
		ActiveTcpConns:     4,
		ActiveUniqueIps:    5,
		QuotaUsedBytes:     1_234_567,
		QuotaLastResetUnix: 1_715_000_000,
	}}

	out, resolved, skipped := g.convertClientUsageSnapshots("agent-quota", wire, now)
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
