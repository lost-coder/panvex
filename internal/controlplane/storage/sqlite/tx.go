package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Transact runs fn inside a single SQLite transaction. BEGIN IMMEDIATE
// acquires the writer lock up front so the first write inside fn cannot
// fail with SQLITE_BUSY mid-transaction. On fn error or panic the
// transaction rolls back; on success it commits. SQLite is a single-
// writer engine, so there is no serialization-retry loop: contention
// is absorbed by busy_timeout at the connection level (see the pragmas
// above). See storage.Store.Transact for the full contract.
func (s *Store) Transact(ctx context.Context, fn storage.TxFn) (retErr error) {
	if s.sqlDB == nil {
		return storage.ErrNestedTransact
	}
	if fn == nil {
		return fmt.Errorf("sqlite: Transact requires a non-nil TxFn")
	}

	// BEGIN IMMEDIATE cannot be issued through BeginTx's options surface;
	// we issue it explicitly on a dedicated connection, run fn against a
	// tx-bound Store, then COMMIT / ROLLBACK on the same conn.
	conn, err := s.sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}

	committed := false
	// ROLLBACK runs in defer and must complete even when the caller's ctx
	// has already been canceled — otherwise we'd leave the writer lock
	// held. context.Background() is intentional here.
	defer func() { //nolint:contextcheck // deferred cleanup must outlive caller ctx
		if p := recover(); p != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
			panic(p)
		}
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	txStore := &Store{db: connExecutor{conn: conn}, sqlDB: nil}
	if err := fn(txStore); err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

// connExecutor adapts *sql.Conn to the dbExecutor interface. *sql.Conn
// already exposes ExecContext/QueryContext/QueryRowContext, but under
// different method set ownership rules; wrapping keeps tx-bound Stores
// honest: callers cannot reach through to BeginTx or Close.
type connExecutor struct {
	conn *sql.Conn
}

func (c connExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.conn.ExecContext(ctx, query, args...)
}

func (c connExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

func (c connExecutor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.conn.QueryRowContext(ctx, query, args...)
}

// txHandle abstracts the commit/rollback surface of *sql.Tx so that
// internal per-method transactions (e.g. ConsumeEnrollmentToken) can
// either open a fresh tx (top-level Store) or reuse the caller's tx
// (Store bound inside Transact) without changing method bodies.
type txHandle interface {
	dbExecutor
	Commit() error
	Rollback() error
}

// passthroughTx wraps an already-open transaction so that Commit /
// Rollback are no-ops: the outer Transact owns the transaction
// lifecycle and must not have it closed out from under it.
type passthroughTx struct {
	dbExecutor
}

func (p passthroughTx) Commit() error   { return nil }
func (p passthroughTx) Rollback() error { return nil }

// beginInternalTx returns a txHandle the caller can drive. When the
// Store is top-level it starts a new transaction; when the Store is
// already inside a Transact (sqlDB == nil) it returns a passthrough
// that reuses the current executor, so the caller's writes land in
// the outer transaction.
func (s *Store) beginInternalTx(ctx context.Context) (txHandle, error) {
	if s.sqlDB == nil {
		return passthroughTx{dbExecutor: s.db}, nil
	}
	return s.sqlDB.BeginTx(ctx, nil)
}
