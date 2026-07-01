package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// RunJSONValidationContract verifies that malformed JSON is rejected at
// write time on the `config` columns (integration_providers,
// fleet_group_integrations) reachable through the typed Store API with a
// caller-supplied raw []byte (M3). This is true parity: on PostgreSQL
// `config` is JSONB, so the driver/server reject malformed input during
// INSERT natively. On SQLite these columns are plain TEXT with no
// engine-level validation, so a `json_valid(...)` CHECK constraint is the
// compensating control (see db/migrations/sqlite
// 0052_json_valid_checks.sql).
//
// jobs.payload_json is deliberately NOT covered here even though it is
// also a raw-string write path: it is plain TEXT on PostgreSQL too (never
// converted to JSONB), so PostgreSQL does not reject malformed JSON there
// either — see RunSQLiteOnlyJSONValidationContract, which documents and
// tests the SQLite-only CHECK added for that column as unilateral
// hardening rather than backend parity.
//
// Not every JSON column is exercised here: `audit_events.details`
// (map[string]any), `metric_snapshots.values` (map[string]uint64), and
// the `connection_links` columns on client_deployments/discovered_clients
// ([]string) are always produced by json.Marshal inside the Store
// implementation before they reach the database — the typed API gives a
// caller no way to hand the store a malformed string for those columns,
// on either backend. Coverage there is structural (encodeStringArray /
// json.Marshal can't produce invalid JSON), not a CHECK-driven test.
func RunJSONValidationContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("CreateIntegrationProvider rejects malformed config", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		err := store.CreateIntegrationProvider(ctx, storage.IntegrationProviderRecord{
			// PostgreSQL's id column is UUID (migration 0014) — SQLite
			// keeps id as TEXT, but the shared contract must use a value
			// valid on both backends.
			ID:        "00000000-0000-4000-a000-000000000101",
			Kind:      "cloudflare",
			Label:     "test",
			Config:    []byte("{not json"),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		})
		if err == nil {
			t.Fatal("CreateIntegrationProvider() with malformed config: got nil error, want rejection")
		}
	})

	t.Run("CreateIntegrationProvider accepts valid config", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		err := store.CreateIntegrationProvider(ctx, storage.IntegrationProviderRecord{
			ID:        "00000000-0000-4000-a000-000000000102",
			Kind:      "cloudflare",
			Label:     "test",
			Config:    []byte(`{"account_id":"abc"}`),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("CreateIntegrationProvider() with valid config: %v", err)
		}
	})

	t.Run("CreateFleetGroupIntegration rejects malformed config", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		seedFleetGroupForJSONContract(t, ctx, store)

		err := store.CreateFleetGroupIntegration(ctx, storage.FleetGroupIntegrationRecord{
			ID:           "00000000-0000-4000-a000-000000000201",
			FleetGroupID: testFleetGroupID,
			Kind:         "cloudflare",
			Config:       []byte("{not json"),
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		})
		if err == nil {
			t.Fatal("CreateFleetGroupIntegration() with malformed config: got nil error, want rejection")
		}
	})

	t.Run("CreateFleetGroupIntegration accepts valid config", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		seedFleetGroupForJSONContract(t, ctx, store)

		err := store.CreateFleetGroupIntegration(ctx, storage.FleetGroupIntegrationRecord{
			ID:           "00000000-0000-4000-a000-000000000202",
			FleetGroupID: testFleetGroupID,
			Kind:         "cloudflare",
			Config:       []byte(`{"zone_id":"abc"}`),
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		})
		if err != nil {
			t.Fatalf("CreateFleetGroupIntegration() with valid config: %v", err)
		}
	})
}

// RunSQLiteOnlyJSONValidationContract verifies SQLite-only json_valid CHECK
// behavior that has no PostgreSQL equivalent to compare against (M3):
//
//   - jobs.payload_json rejects malformed JSON. This is NOT a
//     cross-backend parity assertion: payload_json is plain TEXT on both
//     SQLite and PostgreSQL (PostgreSQL never promoted it to JSONB), so
//     PostgreSQL accepts malformed JSON there today.
//   - integration_providers.config accepts a vault-sealed "PVSn:"
//     ciphertext string alongside plain JSON. fleet.Service transparently
//     seals this column's plaintext when a secretvault is configured
//     (SetVault), independent of storage backend. The permissive CHECK in
//     0052_json_valid_checks.sql (`json_valid(config) OR config LIKE
//     'PVS_:%'`) exists so a live install with vault encryption enabled
//     can still write providers. This is SQLite-only coverage because
//     PostgreSQL's config column is JSONB and a PVSn: string is not valid
//     JSON — encrypting a provider config with a PostgreSQL-backed store
//     already fails today regardless of this migration (pre-existing,
//     out of scope for M3).
//
// Callers must not run this against a PostgreSQL-backed store — both
// sub-tests assume SQLite-specific behavior.
func RunSQLiteOnlyJSONValidationContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("PutJob rejects malformed payload_json", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		err := store.PutJob(ctx, storage.JobRecord{
			ID:             "job-malformed-json",
			Action:         "runtime.reload",
			ActorID:        "user-1",
			Status:         "queued",
			CreatedAt:      time.Now(),
			TTL:            time.Minute,
			IdempotencyKey: "job-malformed-json-key",
			PayloadJSON:    "{not json",
		})
		if err == nil {
			t.Fatal("PutJob() with malformed payload_json: got nil error, want rejection")
		}
	})

	t.Run("PutJob accepts valid payload_json", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		err := store.PutJob(ctx, storage.JobRecord{
			ID:             "job-valid-json",
			Action:         "runtime.reload",
			ActorID:        "user-1",
			Status:         "queued",
			CreatedAt:      time.Now(),
			TTL:            time.Minute,
			IdempotencyKey: "job-valid-json-key",
			PayloadJSON:    `{"key":"value"}`,
		})
		if err != nil {
			t.Fatalf("PutJob() with valid payload_json: %v", err)
		}
	})

	t.Run("PutJob accepts empty payload_json default", func(t *testing.T) {
		// payload_json's zero value ("") predates this contract and is
		// still a live default in Go call sites that never set it. The
		// CHECK must not reject the empty string.
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		err := store.PutJob(ctx, storage.JobRecord{
			ID:             "job-empty-payload",
			Action:         "runtime.reload",
			ActorID:        "user-1",
			Status:         "queued",
			CreatedAt:      time.Now(),
			TTL:            time.Minute,
			IdempotencyKey: "job-empty-payload-key",
			PayloadJSON:    "",
		})
		if err != nil {
			t.Fatalf("PutJob() with empty payload_json: %v", err)
		}
	})

	t.Run("CreateIntegrationProvider accepts vault-sealed config", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		err := store.CreateIntegrationProvider(ctx, storage.IntegrationProviderRecord{
			ID:        "00000000-0000-4000-a000-000000000103",
			Kind:      "cloudflare",
			Label:     "test",
			Config:    []byte("PVS3:sealed-ciphertext-not-json"),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("CreateIntegrationProvider() with vault-sealed config: %v", err)
		}
	})
}

func seedFleetGroupForJSONContract(t *testing.T, ctx context.Context, store storage.MigrationStore) {
	t.Helper()
	if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
		ID:        testFleetGroupID,
		Name:      "json-contract-group",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}
}
