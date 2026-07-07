package storagetest

import (
	"context"
	"database/sql"
	"fmt"
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
//
// enrollment_events.fields_json and webhook_outbox.payload are also JSONB
// on PostgreSQL (db/migrations/postgres/0041_enrollment_attempts.sql,
// 0039_webhook_outbox.sql) and gained a matching json_valid CHECK on
// SQLite in 0052_json_valid_checks.sql, so they belong in this
// cross-backend parity contract too. Neither table is reachable through
// storage.MigrationStore — enrollment timeline events and the webhook
// outbox are written by separate stores (enrollment.SQLStore,
// sqlite/postgres WebhookStore) that take a raw *sql.DB rather than the
// Store interface — so these two subtests fall back to a direct INSERT
// against the pooled connection exposed by the backend's DB() method.
//
// integration_providers.config additionally accepts a vault-sealed
// "PVSn:"-prefixed ciphertext string alongside plain JSON on BOTH engines
// (see the "CreateIntegrationProvider accepts vault-sealed config" subtest
// below) — this used to be SQLite-only coverage until
// db/migrations/postgres/0052_integration_providers_config_text.sql fixed
// the PostgreSQL side to match (bug1/H-6).
func RunJSONValidationContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("CreateIntegrationProvider rejects malformed config", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		err := store.CreateIntegrationProvider(ctx, storage.IntegrationProviderRecord{
			// PostgreSQL's id column is UUID — SQLite
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

	// CreateIntegrationProvider accepts a vault-sealed config (H-6/bug1):
	// fleet.Service.encryptProviderConfig seals integration_providers.config
	// under the vault's "integration_config" domain whenever a secretvault
	// is configured (SetVault), producing a "PVS1:"/"PVS2:"/"PVS3:" prefixed
	// ciphertext string instead of JSON. This is cross-backend parity, not
	// SQLite-only behavior: integration_providers.config is TEXT with a
	// permissive `json_valid(config) OR config LIKE 'PVS_:%'`-equivalent
	// CHECK on BOTH engines (db/migrations/sqlite/0052_json_valid_checks.sql
	// on SQLite; db/migrations/postgres/0052_integration_providers_config_text.sql
	// on PostgreSQL, which also converts the column from JSONB to TEXT so a
	// non-JSON ciphertext string can be stored at all). Before that
	// PostgreSQL migration, this subtest failed on PostgreSQL with a
	// "invalid input syntax for type json" error from the `config::jsonb`
	// cast in postgres/integrations.go — see that migration's header for
	// the full bug writeup. fleet_group_integrations.config is NOT covered
	// here: only provider configs ever get vault-sealed
	// (encryptProviderConfig/decryptProviderConfig in fleet/service.go
	// apply exclusively to CreateProvider/UpdateProvider/GetProvider, never
	// to fleet-group-integration configs), so that column stays strict
	// JSON-only on both engines.
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

		got, err := store.GetIntegrationProvider(ctx, "00000000-0000-4000-a000-000000000103")
		if err != nil {
			t.Fatalf("GetIntegrationProvider() after vault-sealed create: %v", err)
		}
		if string(got.Config) != "PVS3:sealed-ciphertext-not-json" {
			t.Fatalf("GetIntegrationProvider() config = %q, want %q", got.Config, "PVS3:sealed-ciphertext-not-json")
		}

		updated := got
		updated.Label = "test-updated"
		updated.Config = []byte("PVS3:rotated-ciphertext-not-json")
		updated.UpdatedAt = time.Now()
		if err := store.UpdateIntegrationProvider(ctx, updated); err != nil {
			t.Fatalf("UpdateIntegrationProvider() with vault-sealed config: %v", err)
		}

		gotAfterUpdate, err := store.GetIntegrationProvider(ctx, "00000000-0000-4000-a000-000000000103")
		if err != nil {
			t.Fatalf("GetIntegrationProvider() after vault-sealed update: %v", err)
		}
		if string(gotAfterUpdate.Config) != "PVS3:rotated-ciphertext-not-json" {
			t.Fatalf("GetIntegrationProvider() config after update = %q, want %q", gotAfterUpdate.Config, "PVS3:rotated-ciphertext-not-json")
		}
	})

	t.Run("enrollment_events rejects malformed fields_json", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		attemptID := seedEnrollmentAttemptForJSONContract(t, ctx, dbHandle(t, store), "00000000-0000-4000-b000-000000000301")

		err := insertEnrollmentEvent(ctx, dbHandle(t, store), "00000000-0000-4000-b000-000000000302", attemptID, sql.NullString{String: "{not json", Valid: true})
		if err == nil {
			t.Fatal("insert enrollment_events with malformed fields_json: got nil error, want rejection")
		}
	})

	t.Run("enrollment_events accepts valid fields_json", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		attemptID := seedEnrollmentAttemptForJSONContract(t, ctx, dbHandle(t, store), "00000000-0000-4000-b000-000000000303")

		err := insertEnrollmentEvent(ctx, dbHandle(t, store), "00000000-0000-4000-b000-000000000304", attemptID, sql.NullString{String: `{"reason":"ok"}`, Valid: true})
		if err != nil {
			t.Fatalf("insert enrollment_events with valid fields_json: %v", err)
		}
	})

	t.Run("enrollment_events accepts NULL fields_json", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		attemptID := seedEnrollmentAttemptForJSONContract(t, ctx, dbHandle(t, store), "00000000-0000-4000-b000-000000000305")

		err := insertEnrollmentEvent(ctx, dbHandle(t, store), "00000000-0000-4000-b000-000000000306", attemptID, sql.NullString{})
		if err != nil {
			t.Fatalf("insert enrollment_events with NULL fields_json: %v", err)
		}
	})

	t.Run("webhook_outbox rejects malformed payload", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		endpointID := seedWebhookEndpointForJSONContract(t, ctx, dbHandle(t, store), "00000000-0000-4000-b000-000000000401")

		err := insertWebhookOutbox(ctx, dbHandle(t, store), "00000000-0000-4000-b000-000000000402", endpointID, "{not json")
		if err == nil {
			t.Fatal("insert webhook_outbox with malformed payload: got nil error, want rejection")
		}
	})

	t.Run("webhook_outbox accepts valid payload", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		endpointID := seedWebhookEndpointForJSONContract(t, ctx, dbHandle(t, store), "00000000-0000-4000-b000-000000000403")

		err := insertWebhookOutbox(ctx, dbHandle(t, store), "00000000-0000-4000-b000-000000000404", endpointID, `{"action":"agent.created"}`)
		if err != nil {
			t.Fatalf("insert webhook_outbox with valid payload: %v", err)
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
//
// integration_providers.config's vault-sealed "PVSn:" ciphertext coverage
// used to live here (SQLite-only, because PostgreSQL's config column was
// JSONB and rejected non-JSON ciphertext). That asymmetry was the bug
// (bug1/H-6): PostgreSQL's config column is now TEXT with a matching
// permissive CHECK (db/migrations/postgres/0052_integration_providers_config_text.sql),
// so that subtest moved to RunJSONValidationContract as a real cross-backend
// parity assertion — see the doc comment on that subtest for the full
// writeup.
//
// Callers must not run this against a PostgreSQL-backed store — the
// remaining sub-tests assume SQLite-specific behavior.
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

// dbHandleStore is satisfied by both concrete backends (sqlite.Store,
// postgres.Store) but is deliberately not part of storage.MigrationStore
// (see that interface's doc comment: production code must not grow a
// dependency on the raw pool). The two subtests below need it because
// enrollment_events / webhook_outbox are written by separate stores that
// take a *sql.DB directly rather than the Store interface, so there is no
// typed method on MigrationStore to reach them through.
type dbHandleStore interface {
	DB() *sql.DB
}

// dbHandle extracts the raw connection pool from a storage.MigrationStore
// for the direct-SQL fallback inserts used by the enrollment_events /
// webhook_outbox subtests. Fails the test if the concrete backend doesn't
// expose one (it always does today for sqlite.Store and postgres.Store).
func dbHandle(t *testing.T, store storage.MigrationStore) *sql.DB {
	t.Helper()
	h, ok := store.(dbHandleStore)
	if !ok {
		t.Fatalf("store %T does not expose DB() *sql.DB", store)
	}
	return h.DB()
}

// isPostgresHandle distinguishes the two backends by their registered
// database/sql driver type name (pgx's stdlib driver vs modernc.org/sqlite)
// so the direct-SQL helpers below can pick the right placeholder syntax —
// pgx requires "$1"-style params and rejects "?", SQLite is the reverse.
func isPostgresHandle(db *sql.DB) bool {
	return fmt.Sprintf("%T", db.Driver()) == "*stdlib.Driver"
}

// seedEnrollmentAttemptForJSONContract inserts a minimal in_progress
// enrollment_attempts row so the fields_json subtests have a valid
// attempt_id to satisfy enrollment_events' FK. Returns the attempt id.
func seedEnrollmentAttemptForJSONContract(t *testing.T, ctx context.Context, db *sql.DB, attemptID string) string {
	t.Helper()
	now := time.Now().UTC()
	var err error
	if isPostgresHandle(db) {
		_, err = db.ExecContext(ctx, `
			INSERT INTO enrollment_attempts (id, mode, request_id, status, started_at)
			VALUES ($1, 'inbound', $2, 'in_progress', $3)
		`, attemptID, "json-contract-"+attemptID, now)
	} else {
		_, err = db.ExecContext(ctx, `
			INSERT INTO enrollment_attempts (id, mode, request_id, status, started_at)
			VALUES (?, 'inbound', ?, 'in_progress', ?)
		`, attemptID, "json-contract-"+attemptID, now)
	}
	if err != nil {
		t.Fatalf("seed enrollment_attempts: %v", err)
	}
	return attemptID
}

// insertEnrollmentEvent inserts one enrollment_events row directly,
// bypassing enrollment.SQLStore.AppendEvent (which never lets a caller
// supply malformed JSON — see recorder.go) so the json_valid CHECK /
// JSONB rejection can be exercised at the storage layer itself. A
// !fieldsJSON.Valid sql.NullString binds a SQL NULL, matching what
// AppendEvent does today for the empty-fields case.
func insertEnrollmentEvent(ctx context.Context, db *sql.DB, id, attemptID string, fieldsJSON sql.NullString) error {
	now := time.Now().UTC()
	var raw any
	if fieldsJSON.Valid {
		raw = fieldsJSON.String
	}
	if isPostgresHandle(db) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO enrollment_events (attempt_id, ts, step, level, message, fields_json)
			VALUES ($1, $2, 'validate', 'info', $3, $4)
		`, attemptID, now, "json-contract-"+id, raw)
		return err
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO enrollment_events (attempt_id, ts, step, level, message, fields_json)
		VALUES (?, ?, 'validate', 'info', ?, ?)
	`, attemptID, now, "json-contract-"+id, raw)
	return err
}

// seedWebhookEndpointForJSONContract inserts a minimal webhook_endpoints
// row so the payload subtests have a valid endpoint_id to satisfy
// webhook_outbox's FK. Returns the endpoint id.
func seedWebhookEndpointForJSONContract(t *testing.T, ctx context.Context, db *sql.DB, endpointID string) string {
	t.Helper()
	var err error
	if isPostgresHandle(db) {
		_, err = db.ExecContext(ctx, `
			INSERT INTO webhook_endpoints (id, name, url, secret_ciphertext)
			VALUES ($1, $2, 'https://example.invalid/hook', 'ciphertext')
		`, endpointID, "json-contract-"+endpointID)
	} else {
		_, err = db.ExecContext(ctx, `
			INSERT INTO webhook_endpoints (id, name, url, secret_ciphertext)
			VALUES (?, ?, 'https://example.invalid/hook', 'ciphertext')
		`, endpointID, "json-contract-"+endpointID)
	}
	if err != nil {
		t.Fatalf("seed webhook_endpoints: %v", err)
	}
	return endpointID
}

// insertWebhookOutbox inserts one webhook_outbox row directly, bypassing
// WebhookStore.InsertOutbox (which substitutes "{}" for an empty payload
// but never rejects a malformed one — see sqlite/webhooks.go) so the
// json_valid CHECK / JSONB rejection can be exercised at the storage
// layer itself.
func insertWebhookOutbox(ctx context.Context, db *sql.DB, id, endpointID, payload string) error {
	now := time.Now().UTC()
	if isPostgresHandle(db) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO webhook_outbox (id, endpoint_id, event_action, payload, next_attempt_at)
			VALUES ($1, $2, 'agent.created', $3, $4)
		`, id, endpointID, payload, now)
		return err
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO webhook_outbox (id, endpoint_id, event_action, payload, next_attempt_at)
		VALUES (?, ?, 'agent.created', ?, ?)
	`, id, endpointID, payload, now)
	return err
}
