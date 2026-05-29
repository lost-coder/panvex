package settings

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	// register the modernc sqlite driver under "sqlite" for database/sql
	_ "modernc.org/sqlite"
)

// openTestDB opens a temp-file sqlite database with all migrations applied
// and returns the raw *sql.DB. The DB is closed automatically when the test
// ends via t.Cleanup.
//
// We open the raw *sql.DB directly (via sql.Open + sqlite.MigrateContext)
// rather than going through sqlite.Store so that the test stays in the
// settings package without introducing a dependency on sqlite.Store internals.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := filepath.Join(t.TempDir(), "panvex.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := sqlite.MigrateContext(context.Background(), db); err != nil {
		t.Fatalf("MigrateContext: %v", err)
	}

	return db
}

func TestDBStore_RoundTrip(t *testing.T) {
	db := openTestDB(t)
	store := NewDBStore(db, PlaceholderQ)
	ctx := context.Background()

	// --- panel column round-trip ---
	if err := store.WritePanelColumn(ctx, "password_min_length", "14", "test"); err != nil {
		t.Fatalf("WritePanelColumn: %v", err)
	}
	got, err := store.ReadPanelColumn(ctx, "password_min_length")
	if err != nil {
		t.Fatalf("ReadPanelColumn: %v", err)
	}
	if got != "14" {
		t.Errorf("ReadPanelColumn = %q, want %q", got, "14")
	}

	// --- runtime setting round-trip ---
	if err := store.WriteRuntimeSetting(ctx, "updates.channel", `"beta"`, "test"); err != nil {
		t.Fatalf("WriteRuntimeSetting: %v", err)
	}
	val, _, who, err := store.ReadRuntimeSetting(ctx, "updates.channel")
	if err != nil {
		t.Fatalf("ReadRuntimeSetting: %v", err)
	}
	if val != `"beta"` {
		t.Errorf("ReadRuntimeSetting value = %q, want %q", val, `"beta"`)
	}
	if who != "test" {
		t.Errorf("ReadRuntimeSetting who = %q, want %q", who, "test")
	}
}

func TestDBStore_ReadPanelColumn_Missing(t *testing.T) {
	db := openTestDB(t)
	store := NewDBStore(db, PlaceholderQ)
	ctx := context.Background()

	// No row yet — should return "" with nil error.
	got, err := store.ReadPanelColumn(ctx, "http_public_url")
	if err != nil {
		t.Fatalf("ReadPanelColumn on missing row: %v", err)
	}
	if got != "" {
		t.Errorf("ReadPanelColumn = %q, want empty string", got)
	}
}

func TestDBStore_ReadPanelColumn_InvalidColumn(t *testing.T) {
	db := openTestDB(t)
	store := NewDBStore(db, PlaceholderQ)
	ctx := context.Background()

	_, err := store.ReadPanelColumn(ctx, "nonexistent_col")
	if err == nil {
		t.Fatal("expected error for invalid column, got nil")
	}
}

func TestDBStore_WritePanelColumn_InvalidColumn(t *testing.T) {
	db := openTestDB(t)
	store := NewDBStore(db, PlaceholderQ)
	ctx := context.Background()

	err := store.WritePanelColumn(ctx, "drop_table_foo", "evil", "test")
	if err == nil {
		t.Fatal("expected error for invalid column, got nil")
	}
}

// TestDBStore_WritePanelColumn_PasswordMinLength_IntBind exercises the
// int-bind conversion path for password_min_length (the only INTEGER column on
// panel_settings). SQLite coerces a string silently so it won't catch the
// postgres-specific type error, but this guards the strconv conversion + the
// round-trip. Postgres (int4 param) is the real beneficiary of binding an int
// rather than a Go string; that path is exercised by the CI matrix's
// PANVEX_POSTGRES_TEST_DSN storage suite, not here.
func TestDBStore_WritePanelColumn_PasswordMinLength_IntBind(t *testing.T) {
	db := openTestDB(t)
	store := NewDBStore(db, PlaceholderQ)
	ctx := context.Background()

	if err := store.WritePanelColumn(ctx, "password_min_length", "12", ""); err != nil {
		t.Fatalf("WritePanelColumn(password_min_length, 12): %v", err)
	}
	got, err := store.ReadPanelColumn(ctx, "password_min_length")
	if err != nil {
		t.Fatalf("ReadPanelColumn: %v", err)
	}
	if got != "12" {
		t.Errorf("ReadPanelColumn = %q, want %q", got, "12")
	}

	// A non-numeric value must now be rejected before the UPDATE rather than
	// being silently coerced (sqlite) or failing at the driver (postgres).
	if err := store.WritePanelColumn(ctx, "password_min_length", "abc", ""); err == nil {
		t.Fatal("expected error for non-numeric password_min_length, got nil")
	}
}

// TestDBStore_PlaceholderRendering verifies the p() helper without a real DB.
// Postgres round-trip coverage relies on the CI matrix running
// PANVEX_POSTGRES_TEST_DSN against the broader storage test suite, which
// exercises OperationalStore → DBStore via the wiring in lifecycle.go.
func TestDBStore_PlaceholderRendering(t *testing.T) {
	dollar := &DBStore{ph: PlaceholderDollar}
	for _, tc := range []struct {
		n    int
		want string
	}{
		{1, "$1"},
		{2, "$2"},
		{3, "$3"},
	} {
		if got := dollar.p(tc.n); got != tc.want {
			t.Errorf("PlaceholderDollar p(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}

	q := &DBStore{ph: PlaceholderQ}
	for _, n := range []int{1, 2, 7} {
		if got := q.p(n); got != "?" {
			t.Errorf("PlaceholderQ p(%d) = %q, want %q", n, got, "?")
		}
	}
}
