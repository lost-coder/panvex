package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	// ErrDSNRequired reports a missing PostgreSQL connection string.
	ErrDSNRequired = errors.New("postgres dsn is required")
)

// dbExecutor abstracts the query surface shared by *sql.DB and *sql.Tx so
// that Store methods compose inside Transact without duplication. See
// P2-ARCH-01 for the design rationale.
type dbExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store persists control-plane records in a PostgreSQL database.
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

// Open opens a PostgreSQL connection, applies the schema, and returns a storage backend.
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
	if strings.TrimSpace(dsn) == "" {
		return nil, ErrDSNRequired
	}

	poolCfg, err := loadPoolConfigFromEnv()
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(poolCfg.MaxOpenConns)
	db.SetMaxIdleConns(poolCfg.MaxIdleConns)
	db.SetConnMaxLifetime(poolCfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(poolCfg.ConnMaxIdleTime)

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
		// tx-bound store; a live transaction implies a live connection
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
