package sqlite

import (
	"context"
	"database/sql"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// instrumentedExecutor wraps a dbExecutor and increments the per-request DB
// query counter (storage.IncrementDBQuery) on every Exec/Query/QueryRow call.
// HTTP middleware reads the counter at end-of-request to spot N+1 patterns
// (audit P-02). Calls outside a tracked HTTP request are unaffected.
type instrumentedExecutor struct {
	inner dbExecutor
}

func newInstrumentedExecutor(inner dbExecutor) *instrumentedExecutor {
	return &instrumentedExecutor{inner: inner}
}

func (e *instrumentedExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	storage.IncrementDBQuery(ctx)
	return e.inner.ExecContext(ctx, query, args...)
}

func (e *instrumentedExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	storage.IncrementDBQuery(ctx)
	return e.inner.QueryContext(ctx, query, args...)
}

func (e *instrumentedExecutor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	storage.IncrementDBQuery(ctx)
	return e.inner.QueryRowContext(ctx, query, args...)
}
