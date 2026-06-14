package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runEnrollmentContract extracts the enrollment-token + agent-recovery-grant contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runEnrollmentContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("enrollment token create and use round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		// P2-DB-03: enrollment_tokens.fleet_group_id is a FK (ON DELETE
		// SET NULL); the referenced fleet group must exist before we can
		// persist a token that points at it.
		if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf(errPutFleetGroupLong, err)
		}
		token := storage.EnrollmentTokenRecord{
			Value:        "token-value",
			FleetGroupID: testFleetGroupID,
			IssuedAt:     time.Date(2026, time.March, 15, 8, 5, 0, 0, time.UTC),
			ExpiresAt:    time.Date(2026, time.March, 15, 8, 15, 0, 0, time.UTC),
		}

		if err := store.PutEnrollmentToken(ctx, token); err != nil {
			t.Fatalf("PutEnrollmentToken() error = %v", err)
		}

		loadedToken, err := store.GetEnrollmentToken(ctx, token.Value)
		if err != nil {
			t.Fatalf("GetEnrollmentToken() error = %v", err)
		}
		if loadedToken.FleetGroupID != token.FleetGroupID {
			t.Fatalf("GetEnrollmentToken() FleetGroupID = %q, want %q", loadedToken.FleetGroupID, token.FleetGroupID)
		}

		consumedAt := time.Date(2026, time.March, 15, 8, 10, 0, 0, time.UTC)
		consumed, err := store.ConsumeEnrollmentToken(ctx, token.Value, consumedAt)
		if err != nil {
			t.Fatalf("ConsumeEnrollmentToken() error = %v", err)
		}

		if consumed.ConsumedAt == nil || !consumed.ConsumedAt.Equal(consumedAt) {
			t.Fatalf("ConsumeEnrollmentToken() ConsumedAt = %v, want %v", consumed.ConsumedAt, consumedAt)
		}
	})

	t.Run("enrollment token revoke state round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		// P2-DB-03: see note on preceding token test — the fleet group
		// must exist for the FK constraint to accept the token.
		if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf(errPutFleetGroupLong, err)
		}
		token := storage.EnrollmentTokenRecord{
			Value:        "token-revoke",
			FleetGroupID: testFleetGroupID,
			IssuedAt:     time.Date(2026, time.March, 15, 8, 30, 0, 0, time.UTC),
			ExpiresAt:    time.Date(2026, time.March, 15, 8, 45, 0, 0, time.UTC),
		}

		if err := store.PutEnrollmentToken(ctx, token); err != nil {
			t.Fatalf("PutEnrollmentToken() error = %v", err)
		}

		revokedAt := time.Date(2026, time.March, 15, 8, 35, 0, 0, time.UTC)
		revoked, err := store.RevokeEnrollmentToken(ctx, token.Value, revokedAt)
		if err != nil {
			t.Fatalf("RevokeEnrollmentToken() error = %v", err)
		}

		if revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(revokedAt) {
			t.Fatalf("RevokeEnrollmentToken() RevokedAt = %v, want %v", revoked.RevokedAt, revokedAt)
		}

		stored, err := store.GetEnrollmentToken(ctx, token.Value)
		if err != nil {
			t.Fatalf("GetEnrollmentToken() error = %v", err)
		}

		if stored.RevokedAt == nil || !stored.RevokedAt.Equal(revokedAt) {
			t.Fatalf("GetEnrollmentToken() RevokedAt = %v, want %v", stored.RevokedAt, revokedAt)
		}
	})


	t.Run("prune enrollment tokens removes only dead rows", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.June, 1, 8, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf(errPutFleetGroupLong, err)
		}

		now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
		cutoff := now.Add(-24 * time.Hour)
		old := now.Add(-48 * time.Hour)
		oldConsumed := old.Add(time.Minute)
		oldRevoked := old.Add(time.Minute)

		put := func(rec storage.EnrollmentTokenRecord) {
			t.Helper()
			if err := store.PutEnrollmentToken(ctx, rec); err != nil {
				t.Fatalf("PutEnrollmentToken(%s) error = %v", rec.Value, err)
			}
		}
		// consumed long ago — pruned
		put(storage.EnrollmentTokenRecord{Value: "tok-consumed-old", FleetGroupID: testFleetGroupID,
			IssuedAt: old, ExpiresAt: old.Add(time.Hour), ConsumedAt: &oldConsumed})
		// revoked long ago — pruned
		put(storage.EnrollmentTokenRecord{Value: "tok-revoked-old", FleetGroupID: testFleetGroupID,
			IssuedAt: old, ExpiresAt: old.Add(time.Hour), RevokedAt: &oldRevoked})
		// expired unconsumed long ago — pruned
		put(storage.EnrollmentTokenRecord{Value: "tok-expired-old", FleetGroupID: testFleetGroupID,
			IssuedAt: old, ExpiresAt: old.Add(time.Hour)})
		// live token — kept
		put(storage.EnrollmentTokenRecord{Value: "tok-live", FleetGroupID: testFleetGroupID,
			IssuedAt: now, ExpiresAt: now.Add(time.Hour)})

		pruned, err := store.PruneEnrollmentTokens(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneEnrollmentTokens() error = %v", err)
		}
		if pruned != 3 {
			t.Fatalf("PruneEnrollmentTokens() = %d, want 3", pruned)
		}
		remaining, err := store.ListEnrollmentTokens(ctx)
		if err != nil {
			t.Fatalf("ListEnrollmentTokens() error = %v", err)
		}
		if len(remaining) != 1 || remaining[0].Value != "tok-live" {
			t.Fatalf("remaining tokens = %+v, want only tok-live", remaining)
		}
	})

	t.Run("agent certificate recovery grant round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 8, 50, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-000001",
			NodeName:     "node-a",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 15, 8, 55, 0, 0, time.UTC),
		}
		grant := storage.AgentCertificateRecoveryGrantRecord{
			AgentID:   agent.ID,
			IssuedBy:  "user-1",
			IssuedAt:  time.Date(2026, time.March, 15, 9, 0, 0, 0, time.UTC),
			ExpiresAt: time.Date(2026, time.March, 15, 9, 15, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf(errPutFleetGroupLong, err)
		}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}
		if err := store.PutAgentCertificateRecoveryGrant(ctx, grant); err != nil {
			t.Fatalf("PutAgentCertificateRecoveryGrant() error = %v", err)
		}

		loadedGrant, err := store.GetAgentCertificateRecoveryGrant(ctx, grant.AgentID)
		if err != nil {
			t.Fatalf("GetAgentCertificateRecoveryGrant() error = %v", err)
		}
		if loadedGrant.IssuedBy != grant.IssuedBy {
			t.Fatalf("GetAgentCertificateRecoveryGrant() IssuedBy = %q, want %q", loadedGrant.IssuedBy, grant.IssuedBy)
		}
		if !loadedGrant.ExpiresAt.Equal(grant.ExpiresAt) {
			t.Fatalf("GetAgentCertificateRecoveryGrant() ExpiresAt = %v, want %v", loadedGrant.ExpiresAt, grant.ExpiresAt)
		}

		usedAt := time.Date(2026, time.March, 15, 9, 5, 0, 0, time.UTC)
		usedGrant, err := store.UseAgentCertificateRecoveryGrant(ctx, grant.AgentID, usedAt)
		if err != nil {
			t.Fatalf("UseAgentCertificateRecoveryGrant() error = %v", err)
		}
		if usedGrant.UsedAt == nil || !usedGrant.UsedAt.Equal(usedAt) {
			t.Fatalf("UseAgentCertificateRecoveryGrant() UsedAt = %v, want %v", usedGrant.UsedAt, usedAt)
		}

		reloadedGrant, err := store.GetAgentCertificateRecoveryGrant(ctx, grant.AgentID)
		if err != nil {
			t.Fatalf("GetAgentCertificateRecoveryGrant() after use error = %v", err)
		}
		if reloadedGrant.UsedAt == nil || !reloadedGrant.UsedAt.Equal(usedAt) {
			t.Fatalf("GetAgentCertificateRecoveryGrant() after use UsedAt = %v, want %v", reloadedGrant.UsedAt, usedAt)
		}
	})

	t.Run("agent certificate recovery grant revoke state round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 9, 20, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-000002",
			NodeName:     "node-b",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 15, 9, 25, 0, 0, time.UTC),
		}
		grant := storage.AgentCertificateRecoveryGrantRecord{
			AgentID:   agent.ID,
			IssuedBy:  "user-2",
			IssuedAt:  time.Date(2026, time.March, 15, 9, 30, 0, 0, time.UTC),
			ExpiresAt: time.Date(2026, time.March, 15, 9, 45, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf(errPutFleetGroupLong, err)
		}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}
		if err := store.PutAgentCertificateRecoveryGrant(ctx, grant); err != nil {
			t.Fatalf("PutAgentCertificateRecoveryGrant() error = %v", err)
		}

		revokedAt := time.Date(2026, time.March, 15, 9, 35, 0, 0, time.UTC)
		revokedGrant, err := store.RevokeAgentCertificateRecoveryGrant(ctx, grant.AgentID, revokedAt)
		if err != nil {
			t.Fatalf("RevokeAgentCertificateRecoveryGrant() error = %v", err)
		}
		if revokedGrant.RevokedAt == nil || !revokedGrant.RevokedAt.Equal(revokedAt) {
			t.Fatalf("RevokeAgentCertificateRecoveryGrant() RevokedAt = %v, want %v", revokedGrant.RevokedAt, revokedAt)
		}

		storedGrant, err := store.GetAgentCertificateRecoveryGrant(ctx, grant.AgentID)
		if err != nil {
			t.Fatalf("GetAgentCertificateRecoveryGrant() after revoke error = %v", err)
		}
		if storedGrant.RevokedAt == nil || !storedGrant.RevokedAt.Equal(revokedAt) {
			t.Fatalf("GetAgentCertificateRecoveryGrant() after revoke RevokedAt = %v, want %v", storedGrant.RevokedAt, revokedAt)
		}
	})


}
