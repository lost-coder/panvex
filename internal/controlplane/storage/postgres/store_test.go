package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/storagetest"
)

func TestOpenRejectsEmptyDSN(t *testing.T) {
	_, err := Open("")
	if !errors.Is(err, ErrDSNRequired) {
		t.Fatalf("Open() error = %v, want %v", err, ErrDSNRequired)
	}
}

func TestStoreContract(t *testing.T) {
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}

	storagetest.RunStoreContract(t, func(t *testing.T) storage.MigrationStore {
		t.Helper()

		store, err := Open(dsn)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		if err := resetForTest(t.Context(), store); err != nil {
			t.Fatalf("resetForTest() error = %v", err)
		}

		return store
	})
}

// TestJSONValidationContract (M3) asserts PostgreSQL's native JSONB
// validation rejects malformed JSON on the `config` columns — the parity
// baseline the SQLite json_valid CHECK constraints
// (0052_json_valid_checks.sql) bring SQLite up to. jobs.payload_json is
// intentionally excluded: it is plain TEXT on PostgreSQL too, so this
// backend does not reject malformed JSON there (see
// RunSQLiteOnlyJSONValidationContract's doc comment).
func TestJSONValidationContract(t *testing.T) {
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}

	storagetest.RunJSONValidationContract(t, func(t *testing.T) storage.MigrationStore {
		t.Helper()

		store, err := Open(dsn)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		if err := resetForTest(t.Context(), store); err != nil {
			t.Fatalf("resetForTest() error = %v", err)
		}

		return store
	})
}

// resetForTest wipes every table so a persistent Postgres instance can run
// the contract suite twice in a row cleanly. It lists EVERY table in
// 0001_init (R4, audit §1.9): the previous list relied on TRUNCATE ... CASCADE
// to reach dependents via FK, but tables with no FK to the listed roots —
// notably client_ip_history (no FK, unlike client_usage which CASCADEs from
// clients/agents) — leaked rows between cases. Keep this in sync with the
// schema. client_ip_history intentionally has no FK to clients so IP history
// survives client deletion; that is why it must be truncated explicitly here.
func resetForTest(ctx context.Context, store *Store) error {
	_, err := store.sqlDB.ExecContext(ctx, `
		TRUNCATE TABLE
			agent_certificate_recovery_grants,
			agent_config_targets,
			agent_fallback_state,
			agent_revocations,
			agents,
			audit_events,
			certificate_authority,
			client_assignments,
			client_deployments,
			client_ip_history,
			client_usage,
			clients,
			config_apply_batch_targets,
			config_apply_batches,
			consumed_totp,
			cp_secrets,
			discovered_clients,
			enrollment_attempts,
			enrollment_events,
			enrollment_tokens,
			fleet_group_integrations,
			fleet_groups,
			integration_providers,
			job_targets,
			jobs,
			login_lockouts,
			metric_snapshots,
			panel_settings,
			runtime_settings,
			sessions,
			telemt_diagnostics_current,
			telemt_instances,
			telemt_runtime_current,
			telemt_runtime_dcs_current,
			telemt_runtime_events,
			telemt_runtime_upstreams_current,
			telemt_security_inventory_current,
			ts_dc_health,
			ts_server_load,
			ts_server_load_hourly,
			update_config,
			user_appearance,
			user_fleet_group_scopes,
			users,
			webhook_endpoints,
			webhook_outbox
		RESTART IDENTITY CASCADE
	`)
	return err
}
