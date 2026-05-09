// internal/controlplane/storage/sqlite/uow.go
//
// SQLite-backed UnitOfWork. The transaction contract mirrors
// (*Store).Transact in tx.go — BEGIN IMMEDIATE acquires the writer lock
// up front; panics inside fn cause rollback + re-raise; no retry loop
// (SQLite is a single-writer engine; contention is absorbed by the
// busy_timeout pragma). Nested Do calls return storage.ErrNestedTransact.
package sqlite

import (
	"context"
	"database/sql"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/uow"
)

// sqUoW is a SQLite-backed UnitOfWork.
type sqUoW struct {
	db *sql.DB
}

// NewUoW constructs a SQLite-backed UnitOfWork bound to the given pool.
// Pass store.DB() for production wiring.
func NewUoW(db *sql.DB) uow.UnitOfWork {
	return &sqUoW{db: db}
}

func (u *sqUoW) Do(ctx context.Context, fn func(rs uow.RepoSet) error) error {
	if u.db == nil {
		return storage.ErrNestedTransact
	}

	// Use a dedicated *sql.Conn so BEGIN IMMEDIATE and COMMIT/ROLLBACK are
	// all sent on the same connection. database/sql's BeginTx does not
	// expose BEGIN IMMEDIATE through TxOptions, so we issue it manually —
	// the same pattern used by (*Store).Transact.
	conn, err := u.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}

	committed := false
	// ROLLBACK must complete even when ctx has been cancelled — otherwise
	// the writer lock stays held. context.Background() is intentional.
	defer func() { //nolint:contextcheck // deferred cleanup must outlive caller ctx
		if p := recover(); p != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
			panic(p) // re-raise
		}
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	rs := newTxRepoSet(connExecutor{conn: conn})
	if err := fn(rs); err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
