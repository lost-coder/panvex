package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestOpenAppliesWALPragma asserts journal_mode is WAL on a freshly opened DB.
// WAL is a database-level setting persisted in the file header, so this
// remains set even for subsequent connections.
func TestOpenAppliesWALPragma(t *testing.T) {
	store := openTestStore(t)

	var journalMode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("journal_mode = %q, want %q", journalMode, "wal")
	}
}

// TestOpenAppliesBusyTimeoutPragma asserts the 5s busy_timeout is applied on
// every pooled connection. We probe multiple connections to confirm this is
// not accidentally limited to the first one.
func TestOpenAppliesBusyTimeoutPragma(t *testing.T) {
	store := openTestStore(t)

	// Hold a few connections concurrently so the pool opens fresh ones and
	// we verify every pooled handle has the pragma.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const probes = 4
	for i := 0; i < probes; i++ {
		conn, err := store.db.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire conn %d: %v", i, err)
		}
		t.Cleanup(func() { _ = conn.Close() })

		var busyTimeout int
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatalf("PRAGMA busy_timeout on conn %d: %v", i, err)
		}
		if busyTimeout != 5000 {
			t.Fatalf("conn %d busy_timeout = %d, want 5000", i, busyTimeout)
		}
	}
}

// TestOpenAppliesForeignKeysPragma asserts foreign_keys = 1 on every pooled
// connection (regression guard for the MaxOpenConns = 1 era).
func TestOpenAppliesForeignKeysPragma(t *testing.T) {
	store := openTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 4; i++ {
		conn, err := store.db.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire conn %d: %v", i, err)
		}
		t.Cleanup(func() { _ = conn.Close() })

		var foreignKeys int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("PRAGMA foreign_keys on conn %d: %v", i, err)
		}
		if foreignKeys != 1 {
			t.Fatalf("conn %d foreign_keys = %d, want 1", i, foreignKeys)
		}
	}
}

// TestOpenAppliesSynchronousPragma asserts synchronous = NORMAL (value 1).
// PRAGMA synchronous returns the numeric code: 0=OFF, 1=NORMAL, 2=FULL, 3=EXTRA.
func TestOpenAppliesSynchronousPragma(t *testing.T) {
	store := openTestStore(t)

	var sync int
	if err := store.db.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil {
		t.Fatalf("PRAGMA synchronous: %v", err)
	}
	if sync != 1 {
		t.Fatalf("synchronous = %d, want 1 (NORMAL)", sync)
	}
}

// TestOpenAppliesTempStorePragma asserts temp_store = MEMORY (value 2).
// PRAGMA temp_store returns the numeric code: 0=DEFAULT, 1=FILE, 2=MEMORY.
func TestOpenAppliesTempStorePragma(t *testing.T) {
	store := openTestStore(t)

	var tempStore int
	if err := store.db.QueryRow("PRAGMA temp_store").Scan(&tempStore); err != nil {
		t.Fatalf("PRAGMA temp_store: %v", err)
	}
	if tempStore != 2 {
		t.Fatalf("temp_store = %d, want 2 (MEMORY)", tempStore)
	}
}

// TestOpenAppliesMmapSizePragma asserts mmap_size = 268435456 (256 MB).
func TestOpenAppliesMmapSizePragma(t *testing.T) {
	store := openTestStore(t)

	var mmapSize int64
	if err := store.db.QueryRow("PRAGMA mmap_size").Scan(&mmapSize); err != nil {
		t.Fatalf("PRAGMA mmap_size: %v", err)
	}
	if mmapSize != 268435456 {
		t.Fatalf("mmap_size = %d, want 268435456", mmapSize)
	}
}

// TestOpenRejectsInMemoryDSN asserts that `:memory:` is rejected. WAL mode
// silently downgrades to journal_mode=memory for in-memory databases, which
// defeats the concurrency guarantees Open() advertises.
func TestOpenRejectsInMemoryDSN(t *testing.T) {
	for _, dsn := range []string{":memory:", "  :memory:  "} {
		store, err := Open(dsn)
		if err == nil {
			_ = store.Close()
			t.Fatalf("Open(%q) error = nil, want non-nil", dsn)
		}
		if !strings.Contains(err.Error(), "in-memory") {
			t.Fatalf("Open(%q) error = %v, want message mentioning in-memory", dsn, err)
		}
	}
}

// TestConcurrentInsertsDoNotTimeOut is a smoke test for DF-17: under WAL +
// busy_timeout, 5 goroutines each inserting 10 rows must all commit within
// the 5s deadline without SQLITE_BUSY. If the pragmas or pool are
// misconfigured, this test either times out or returns "database is locked".
func TestConcurrentInsertsDoNotTimeOut(t *testing.T) {
	store := openTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := store.db.ExecContext(ctx, `
		CREATE TABLE pragma_concurrency_probe (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			writer INTEGER NOT NULL,
			payload TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("create probe table: %v", err)
	}

	const writers = 5
	const rowsPerWriter = 10

	var wg sync.WaitGroup
	errs := make(chan error, writers)

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for r := 0; r < rowsPerWriter; r++ {
				_, err := store.db.ExecContext(ctx,
					`INSERT INTO pragma_concurrency_probe (writer, payload) VALUES (?, ?)`,
					writer, fmt.Sprintf("w%d-r%d", writer, r),
				)
				if err != nil {
					errs <- fmt.Errorf("writer %d row %d: %w", writer, r, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("%v", err)
	}

	var got int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_concurrency_probe`).Scan(&got); err != nil {
		t.Fatalf("count probe rows: %v", err)
	}
	if want := writers * rowsPerWriter; got != want {
		t.Fatalf("probe rows = %d, want %d", got, want)
	}
}

// openTestStore opens an on-disk SQLite store in a temp dir. WAL mode requires
// an actual file — `:memory:` databases cannot use WAL (known SQLite quirk).
func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pragmas.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
