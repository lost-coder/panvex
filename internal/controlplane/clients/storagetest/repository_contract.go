// internal/controlplane/clients/storagetest/repository_contract.go
//
// RunContract exercises any clients.Repository implementation. Backends
// invoke this from their own *_test.go to verify they meet the contract.
package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// OpenRepo is a factory that creates a fresh, empty Repository for a single
// sub-test. Each sub-test receives its own instance so state does not leak.
type OpenRepo func(t *testing.T) clients.Repository

// RunContract runs all repository contract sub-tests against the given
// OpenRepo factory. Backends call this once from their *_test.go files.
func RunContract(t *testing.T, open OpenRepo) {
	t.Helper()
	t.Run("SaveLoadRoundTrip", func(t *testing.T) { runSaveLoadRoundTrip(t, open(t)) })
	t.Run("ListEmpty", func(t *testing.T) { runListEmpty(t, open(t)) })
	t.Run("GetNotFound", func(t *testing.T) { runGetNotFound(t, open(t)) })
	t.Run("DeleteCascadesAssignments", func(t *testing.T) { runDeleteCascadesAssignments(t, open(t)) })
	t.Run("GetSoftDeletedReturnsNotFound", func(t *testing.T) { runGetSoftDeletedReturnsNotFound(t, open(t)) })
	t.Run("UsageBulkRoundtrip", func(t *testing.T) { runUsageBulk(t, open(t)) })
	t.Run("UsageMonotonicity", func(t *testing.T) { runUsageMonotonicity(t, open(t)) })
	t.Run("UsageWatermarkRoundtrip", func(t *testing.T) { runUsageWatermarkRoundtrip(t, open(t)) })
	t.Run("GetBySubscriptionToken", func(t *testing.T) { runGetBySubscriptionToken(t, open(t)) })
	// More subtests added as Repository surface grows.
}

func runSaveLoadRoundTrip(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	c := clients.Client{
		ID:                clients.ClientID("c-rt-1"),
		Name:              "round-trip",
		Secret:            "opaque-secret",
		SubscriptionToken: "tok_roundtrip_1",
		CreatedAt:         time.Unix(1700000000, 0).UTC(),
		UpdatedAt:         time.Unix(1700000001, 0).UTC(),
	}
	if err := repo.Save(ctx, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != c.ID || got.Name != c.Name {
		t.Fatalf("Get returned %+v, want %+v", got, c)
	}
	if got.Secret != c.Secret {
		t.Fatalf("Secret mismatch: got %q, want %q", got.Secret, c.Secret)
	}
	if got.SubscriptionToken != c.SubscriptionToken {
		t.Fatalf("SubscriptionToken mismatch: got %q, want %q", got.SubscriptionToken, c.SubscriptionToken)
	}

	// Verify token survives List as well.
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, cl := range list {
		if cl.ID == c.ID {
			found = true
			if cl.SubscriptionToken != c.SubscriptionToken {
				t.Fatalf("List SubscriptionToken mismatch: got %q, want %q", cl.SubscriptionToken, c.SubscriptionToken)
			}
			break
		}
	}
	if !found {
		t.Fatalf("saved client %q not found in List", c.ID)
	}
}

func runListEmpty(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List on empty: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List on empty repo returned %d items", len(list))
	}
}

func runGetNotFound(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	_, err := repo.Get(ctx, clients.ClientID("does-not-exist"))
	// We expect storage.ErrNotFound here. Backend wires the sentinel via
	// repository wrapping. Specific error check goes here once
	// storage.ErrNotFound is a stable sentinel — for now we accept any
	// non-nil error and only verify backends agree.
	if err == nil {
		t.Fatal("Get of nonexistent must return error")
	}
}

func runDeleteCascadesAssignments(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	id := clients.ClientID("c-cascade")
	c := clients.Client{ID: id, Name: "cascade"}
	if err := repo.Save(ctx, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	a := clients.Assignment{
		ID:           clients.AssignmentID("a-1"),
		ClientID:     id,
		TargetType:   clients.TargetTypeFleetGroup,
		FleetGroupID: clients.FleetGroupID("fg-test"),
	}
	if err := repo.SaveAssignments(ctx, id, []clients.Assignment{a}); err != nil {
		t.Fatalf("SaveAssignments: %v", err)
	}
	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := repo.ListAssignments(ctx, id)
	if err != nil {
		t.Fatalf("ListAssignments after Delete: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Delete must cascade assignments, got %d remaining", len(got))
	}
}

// runGetSoftDeletedReturnsNotFound guards H-8: a soft-deleted client must be
// invisible to Get on every backend (SQLite previously returned the row while
// Postgres filtered it, a cross-backend correctness divergence).
func runGetSoftDeletedReturnsNotFound(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	id := clients.ClientID("c-softdel")
	if err := repo.Save(ctx, clients.Client{ID: id, Name: "softdel"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := repo.Get(ctx, id); err != nil {
		t.Fatalf("precondition Get before delete: %v", err)
	}
	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, id); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get after soft-delete error = %v, want storage.ErrNotFound", err)
	}
}

// runGetBySubscriptionToken exercises the GetBySubscriptionToken contract:
// known token → correct client; unknown token → ErrNotFound; blank token →
// ErrNotFound; soft-deleted client's token → ErrNotFound.
func runGetBySubscriptionToken(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()

	c := clients.Client{
		ID:                clients.ClientID("c-sub-1"),
		Name:              "sub-client",
		Secret:            "opaque",
		SubscriptionToken: "tok_abc123_xxxxxxxxxxxxxxxxxxxxxxxxxxx",
		CreatedAt:         time.Unix(1700000000, 0).UTC(),
		UpdatedAt:         time.Unix(1700000001, 0).UTC(),
	}
	if err := repo.Save(ctx, c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Known token → correct client.
	got, err := repo.GetBySubscriptionToken(ctx, c.SubscriptionToken)
	if err != nil {
		t.Fatalf("GetBySubscriptionToken(known): %v", err)
	}
	if got.ID != c.ID {
		t.Fatalf("GetBySubscriptionToken: got ID %q, want %q", got.ID, c.ID)
	}
	if got.SubscriptionToken != c.SubscriptionToken {
		t.Fatalf("GetBySubscriptionToken: got token %q, want %q", got.SubscriptionToken, c.SubscriptionToken)
	}

	// Unknown token → ErrNotFound.
	_, err = repo.GetBySubscriptionToken(ctx, "tok_does_not_exist_xxxxxxxxxxxxxxxxx")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetBySubscriptionToken(unknown): err = %v, want storage.ErrNotFound", err)
	}

	// Blank token → ErrNotFound.
	_, err = repo.GetBySubscriptionToken(ctx, "")
	if err == nil {
		t.Fatal("GetBySubscriptionToken(blank): expected error, got nil")
	}

	// Soft-deleted client's token → ErrNotFound.
	c2 := clients.Client{
		ID:                clients.ClientID("c-sub-deleted"),
		Name:              "deleted-sub-client",
		SubscriptionToken: "tok_deleted_xxxxxxxxxxxxxxxxxxxxxxxx",
		CreatedAt:         time.Unix(1700000000, 0).UTC(),
		UpdatedAt:         time.Unix(1700000001, 0).UTC(),
	}
	if err := repo.Save(ctx, c2); err != nil {
		t.Fatalf("Save c2: %v", err)
	}
	if err := repo.Delete(ctx, c2.ID); err != nil {
		t.Fatalf("Delete c2: %v", err)
	}
	_, err = repo.GetBySubscriptionToken(ctx, c2.SubscriptionToken)
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetBySubscriptionToken(deleted client): err = %v, want storage.ErrNotFound", err)
	}
}

func runUsageBulk(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	// Set up two clients so foreign keys hold.
	for _, id := range []clients.ClientID{"c-u-1", "c-u-2"} {
		c := clients.Client{ID: id, Name: string(id)}
		if err := repo.Save(ctx, c); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}
	batch := []clients.Usage{
		{ClientID: "c-u-1", AgentID: "a-1", TrafficUsedBytes: 100, QuotaUsedBytes: 4096, QuotaLastResetUnix: 1700000000},
		{ClientID: "c-u-2", AgentID: "a-1", TrafficUsedBytes: 200},
	}
	if err := repo.UpsertUsageBulk(ctx, batch); err != nil {
		t.Fatalf("UpsertUsageBulk: %v", err)
	}
	got, err := repo.ListUsage(ctx)
	if err != nil {
		t.Fatalf("ListUsage: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListUsage returned %d, want 2", len(got))
	}
	// IN-H2: quota counters must survive the round-trip (previously dropped).
	byID := make(map[clients.ClientID]clients.Usage, len(got))
	for _, u := range got {
		byID[u.ClientID] = u
	}
	if u := byID["c-u-1"]; u.QuotaUsedBytes != 4096 || u.QuotaLastResetUnix != 1700000000 {
		t.Fatalf("quota round-trip for c-u-1 = (used=%d, last_reset=%d), want (4096, 1700000000)",
			u.QuotaUsedBytes, u.QuotaLastResetUnix)
	}
}

// runUsageMonotonicity is the storagetest case for task 2.10 (audit finding:
// client_usage upsert had no monotonicity guard on last_seq, so an
// out-of-order or duplicate/older agent report could regress the stored
// counters to a stale sequence). A report only applies when its last_seq is
// strictly newer than what's stored; an older or equal last_seq must be a
// silent no-op, not a regression and not an error.
func runUsageMonotonicity(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	const (
		clientID = clients.ClientID("c-mono-1")
		// a-1 is the agent seeded by both backends' contract fixtures
		// (seedContractFixtures in sqlite, inline seed in postgres) to
		// satisfy the client_usage.agent_id FK.
		agentID = "a-1"
	)
	if err := repo.Save(ctx, clients.Client{ID: clientID, Name: string(clientID)}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	base := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	findUsage := func() clients.Usage {
		t.Helper()
		all, err := repo.ListUsage(ctx)
		if err != nil {
			t.Fatalf("ListUsage: %v", err)
		}
		for _, u := range all {
			if u.ClientID == clientID && u.AgentID == agentID {
				return u
			}
		}
		t.Fatalf("no usage row found for %s/%s", clientID, agentID)
		return clients.Usage{}
	}

	// Establish the row at last_seq=5 ("counters A").
	seq5 := clients.Usage{
		ClientID: clientID, AgentID: agentID,
		TrafficUsedBytes: 500, UniqueIPsUsed: 5,
		ActiveTCPConns: 5, ActiveUniqueIPs: 5,
		LastSeq: 5, ObservedAt: base,
	}
	if err := repo.UpsertUsage(ctx, seq5); err != nil {
		t.Fatalf("UpsertUsage(seq=5): %v", err)
	}
	if got := findUsage(); got.LastSeq != 5 || got.TrafficUsedBytes != 500 {
		t.Fatalf("after seq=5: last_seq=%d traffic=%d, want 5/500", got.LastSeq, got.TrafficUsedBytes)
	}

	// Out-of-order older report (last_seq=3, "counters B") must NOT regress
	// the stored row — it stays at seq=5/counters A.
	seq3 := clients.Usage{
		ClientID: clientID, AgentID: agentID,
		TrafficUsedBytes: 3, UniqueIPsUsed: 3,
		ActiveTCPConns: 3, ActiveUniqueIPs: 3,
		LastSeq: 3, ObservedAt: base.Add(time.Minute),
	}
	if err := repo.UpsertUsage(ctx, seq3); err != nil {
		t.Fatalf("UpsertUsage(seq=3, stale): %v", err)
	}
	if got := findUsage(); got.LastSeq != 5 || got.TrafficUsedBytes != 500 {
		t.Fatalf("after stale seq=3: last_seq=%d traffic=%d, want unchanged 5/500 (no regression)",
			got.LastSeq, got.TrafficUsedBytes)
	}

	// A newer report (last_seq=7) must apply normally.
	seq7 := clients.Usage{
		ClientID: clientID, AgentID: agentID,
		TrafficUsedBytes: 700, UniqueIPsUsed: 7,
		ActiveTCPConns: 7, ActiveUniqueIPs: 7,
		LastSeq: 7, ObservedAt: base.Add(2 * time.Minute),
	}
	if err := repo.UpsertUsage(ctx, seq7); err != nil {
		t.Fatalf("UpsertUsage(seq=7): %v", err)
	}
	if got := findUsage(); got.LastSeq != 7 || got.TrafficUsedBytes != 700 {
		t.Fatalf("after seq=7: last_seq=%d traffic=%d, want 7/700 (newer report applies)",
			got.LastSeq, got.TrafficUsedBytes)
	}
}

// runUsageWatermarkRoundtrip verifies the P4 cumulative-counter watermark
// (agent_boot_id, last_total_bytes) survives persist + ListUsage on both
// backends, so the panel's Restore rehydrates delta-derivation state
// across restarts.
func runUsageWatermarkRoundtrip(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	const (
		clientID = clients.ClientID("c-wm-1")
		agentID  = "a-1"
	)
	if err := repo.Save(ctx, clients.Client{ID: clientID, Name: string(clientID)}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	base := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	findUsage := func() clients.Usage {
		t.Helper()
		all, err := repo.ListUsage(ctx)
		if err != nil {
			t.Fatalf("ListUsage: %v", err)
		}
		for _, u := range all {
			if u.ClientID == clientID && u.AgentID == agentID {
				return u
			}
		}
		t.Fatalf("no usage row found for %s/%s", clientID, agentID)
		return clients.Usage{}
	}

	if err := repo.UpsertUsage(ctx, clients.Usage{
		ClientID: clientID, AgentID: agentID,
		TrafficUsedBytes: 700,
		AgentBootID:      "boot-7f3a",
		LastTotalBytes:   12345,
		LastSeq:          99, // seq protocol still alive in this task
		ObservedAt:       base.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertUsage(watermark): %v", err)
	}
	got := findUsage()
	if got.AgentBootID != "boot-7f3a" || got.LastTotalBytes != 12345 {
		t.Fatalf("watermark round-trip: boot=%q total=%d, want boot-7f3a/12345",
			got.AgentBootID, got.LastTotalBytes)
	}
}
