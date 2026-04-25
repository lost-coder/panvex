package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// sqlitePragmas are applied to every pooled connection via the modernc.org/sqlite
// `_pragma=` DSN parameter. See DF-17 / M-F10 in the remediation plan:
// without WAL + busy_timeout, any concurrent writer produces SQLITE_BUSY and
// bottles the SQLite deployment.
//
//   - journal_mode = WAL ........ Concurrent readers, serialized writers.
//     WAL is a database-level setting persisted in the file header; applying
//     it once upgrades the file permanently. Each connection still reports
//     `wal` when queried, which the tests rely on.
//   - synchronous = NORMAL ...... Recommended companion to WAL. Durable across
//     process crashes; a small window (last committed txn) is exposed to OS /
//     power loss. FULL is overkill under WAL because the WAL itself provides
//     crash-consistency for committed transactions. Accepted trade-off for
//     the control-plane workload — writes are idempotent / re-replayable.
//   - busy_timeout = 5000 ....... 5-second retry budget for lock contention.
//     Without this, SQLite returns SQLITE_BUSY immediately.
//   - foreign_keys = ON ......... FK constraints are off by default in SQLite.
//   - temp_store = MEMORY ....... Temp tables and indexes live in RAM.
//   - mmap_size = 268435456 ..... 256 MB mmap window for reads; reduces read
//     syscalls on hot pages.
var sqlitePragmas = []string{
	"journal_mode=WAL",
	"synchronous=NORMAL",
	"busy_timeout=5000",
	"foreign_keys=ON",
	"temp_store=MEMORY",
	"mmap_size=268435456",
}

// dbExecutor abstracts the query surface shared by *sql.DB and *sql.Tx so
// that Store methods compose inside Transact without duplication. See
// P2-ARCH-01 for the design rationale.
type dbExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store persists control-plane records in a local SQLite database.
//
// Store methods reference s.db via the dbExecutor interface so the same
// method bodies can run against a *sql.DB (outside Transact) or a
// *sql.Tx (inside Transact). s.sqlDB is the pool handle used for
// lifecycle (Ping, Close, BeginTx); it is nil on transaction-bound
// Stores to prevent accidental escape from the transaction boundary.
type Store struct {
	db    dbExecutor
	sqlDB *sql.DB
}

// Open opens a SQLite database file, applies the schema, and returns a storage
// backend.
//
// The DSN must be an on-disk file path. In-memory databases (":memory:") are
// rejected because WAL mode requires a real file — SQLite silently downgrades
// journal_mode to "memory" for in-memory databases, which defeats the
// concurrency guarantees the rest of the control-plane relies on. Tests that
// need a transient database should use `t.TempDir()` + a filename instead.
//
// Open uses context.Background() for migrations and the initial Ping; callers
// that need cancellation during startup should use OpenContext instead.
func Open(dsn string) (*Store, error) {
	return OpenContext(context.Background(), dsn)
}

// OpenContext is the context-aware variant of Open. It threads ctx through
// schema migration and the initial connectivity check so startup work can be
// cancelled by the caller.
func OpenContext(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == ":memory:" {
		return nil, fmt.Errorf("sqlite: in-memory DSN not supported; WAL requires an on-disk file")
	}

	if err := ensureParentDirectory(dsn); err != nil {
		return nil, err
	}

	dsnWithPragmas, err := appendPragmasToDSN(dsn, sqlitePragmas)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsnWithPragmas)
	if err != nil {
		return nil, err
	}

	// WAL permits concurrent readers while a single writer holds the log.
	// Previously MaxOpenConns was pinned to 1 because PRAGMA foreign_keys is
	// per-connection and we had no way to apply it to every pooled handle.
	// Pragmas are now applied via the `_pragma=` DSN parameter (see
	// modernc.org/sqlite conn.newConn -> applyQueryParams), so every
	// connection inherits them on Open. We can safely allow 4 connections:
	// one services writes, the rest serve concurrent reads. The 5s
	// busy_timeout absorbs transient lock contention without surfacing
	// SQLITE_BUSY to callers.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := MigrateContext(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db, sqlDB: db}, nil
}

// Ping verifies that the database connection is alive.
func (s *Store) Ping(ctx context.Context) error {
	if s.sqlDB == nil {
		return nil
	}
	return s.sqlDB.PingContext(ctx)
}

// Close releases the database handle owned by the store.
func (s *Store) Close() error {
	if s.sqlDB == nil {
		return nil
	}
	return s.sqlDB.Close()
}

// PoolStats returns the current sql.DBStats for this store, or the
// zero value when the store is tx-bound (no pool of its own). Used by
// the metrics publisher to expose panvex_db_pool_* gauges.
func (s *Store) PoolStats() sql.DBStats {
	if s.sqlDB == nil {
		return sql.DBStats{}
	}
	return s.sqlDB.Stats()
}

// appendPragmasToDSN rewrites the DSN so that every connection opened by the
// driver applies the given pragmas at startup. modernc.org/sqlite splits the
// DSN on the first '?' and parses everything after it as url.Values, then
// calls `PRAGMA <v>` for each `_pragma=<v>` value on every new connection.
func appendPragmasToDSN(dsn string, pragmas []string) (string, error) {
	path := dsn
	existing := url.Values{}

	if idx := strings.IndexRune(dsn, '?'); idx >= 0 {
		path = dsn[:idx]
		parsed, err := url.ParseQuery(dsn[idx+1:])
		if err != nil {
			return "", err
		}
		existing = parsed
	}

	for _, p := range pragmas {
		existing.Add("_pragma", p)
	}

	return path + "?" + existing.Encode(), nil
}

func ensureParentDirectory(dsn string) error {
	parent := filepath.Dir(dsn)
	if parent == "." || parent == "" {
		return nil
	}

	return os.MkdirAll(parent, 0o755)
}
