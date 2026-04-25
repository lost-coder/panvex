package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// maxTransactRetries caps how many times Transact retries on a PostgreSQL
// serialization failure (SQLSTATE 40001). Beyond this the caller is
// returned the last error; raising the cap risks wedging a request under
// contention. See P2-ARCH-01.
const maxTransactRetries = 3

// Transact runs fn inside a single database transaction with
// read-committed isolation. On serialization failures it retries up
// to maxTransactRetries times. See storage.Store.Transact for the
// full contract.
func (s *Store) Transact(ctx context.Context, fn storage.TxFn) error {
	if s.sqlDB == nil {
		// Reached from a tx-bound Store — nested Transact is forbidden.
		return storage.ErrNestedTransact
	}
	if fn == nil {
		return errors.New("postgres: Transact requires a non-nil TxFn")
	}

	var lastErr error
	for attempt := 0; attempt < maxTransactRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := s.runTransact(ctx, fn)
		if err == nil {
			return nil
		}

		if !isSerializationFailure(err) {
			return err
		}
		lastErr = err

		if attempt == maxTransactRetries-1 {
			break
		}

		// Jittered backoff: (attempt+1)*10ms + 0..10ms jitter.
		// Respects ctx.Done() so cancellation aborts the wait promptly.
		backoff := time.Duration(attempt+1)*10*time.Millisecond + time.Duration(rand.Intn(10))*time.Millisecond //nolint:gosec // backoff jitter, not security-sensitive
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	return fmt.Errorf("postgres: serialization failure after %d retries: %w", maxTransactRetries, lastErr)
}

func (s *Store) runTransact(ctx context.Context, fn storage.TxFn) (retErr error) {
	tx, err := s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}

	txStore := &Store{db: tx, sqlDB: nil}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if retErr != nil {
			_ = tx.Rollback()
			return
		}
		if commitErr := tx.Commit(); commitErr != nil {
			retErr = commitErr
		}
	}()

	if err := fn(txStore); err != nil {
		return err
	}
	return nil
}

// isSerializationFailure reports whether err originates from a
// PostgreSQL serialization_failure (SQLSTATE 40001). pgx surfaces it
// as *pgconn.PgError through database/sql.
func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001"
	}
	return false
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
// Store is top-level it starts a new read-committed transaction;
// when the Store is already inside a Transact (sqlDB == nil) it
// returns a passthrough that reuses the current executor, so the
// caller's writes land in the outer transaction.
func (s *Store) beginInternalTx(ctx context.Context) (txHandle, error) {
	if s.sqlDB == nil {
		return passthroughTx{dbExecutor: s.db}, nil
	}
	return s.sqlDB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}
