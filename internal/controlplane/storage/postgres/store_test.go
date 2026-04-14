package postgres

import (
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

	storagetest.RunStoreContract(t, func(t *testing.T) storage.Store {
		t.Helper()

		store, err := Open(dsn)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		if err := resetForTest(store); err != nil {
			t.Fatalf("resetForTest() error = %v", err)
		}

		return store
	})
}

func resetForTest(store *Store) error {
	_, err := store.db.Exec(`
		TRUNCATE TABLE
			telemt_runtime_events,
			telemt_runtime_upstreams_current,
			telemt_runtime_dcs_current,
			telemt_diagnostics_current,
			telemt_security_inventory_current,
			telemt_runtime_current,
			telemt_detail_boosts,
			job_targets,
			jobs,
			client_deployments,
			client_assignments,
			clients,
			telemt_instances,
			agents,
			fleet_groups,
			users,
			audit_events,
			metric_snapshots,
			enrollment_tokens,
			panel_settings,
			user_appearance,
			certificate_authority
		RESTART IDENTITY CASCADE
	`)
	return err
}
