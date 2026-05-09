// internal/controlplane/storage/postgres/uow.go
//
// Postgres-backed UnitOfWork. The retry and rollback contract mirrors
// (*Store).Transact in tx.go — serialization failures (SQLSTATE 40001)
// are retried up to maxTransactRetries times with jittered backoff;
// panics inside fn cause rollback + re-raise; nested Do returns
// storage.ErrNestedTransact.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/uow"
)

// pgUoW is a postgres-backed UnitOfWork.
type pgUoW struct {
	db *sql.DB
}

// NewUoW constructs a Postgres-backed UnitOfWork bound to the given pool.
// Pass store.DB() for production wiring.
func NewUoW(db *sql.DB) uow.UnitOfWork {
	return &pgUoW{db: db}
}

func (u *pgUoW) Do(ctx context.Context, fn func(rs uow.RepoSet) error) error {
	if u.db == nil {
		// Nested / tx-bound use is not allowed.
		return storage.ErrNestedTransact
	}

	var lastErr error
	for attempt := 0; attempt < maxTransactRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := u.runOnce(ctx, fn)
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
		backoff := time.Duration(attempt+1)*10*time.Millisecond + time.Duration(rand.Intn(10))*time.Millisecond //nolint:gosec // backoff jitter, not security-sensitive
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	return fmt.Errorf("postgres uow: serialization failure after %d retries: %w", maxTransactRetries, lastErr)
}

func (u *pgUoW) runOnce(ctx context.Context, fn func(rs uow.RepoSet) error) (retErr error) {
	tx, err := u.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p) // re-raise
		}
		if retErr != nil {
			_ = tx.Rollback()
			return
		}
		if commitErr := tx.Commit(); commitErr != nil {
			retErr = commitErr
		}
	}()

	rs := newTxRepoSet(tx)
	if err := fn(rs); err != nil {
		return err
	}
	return nil
}
