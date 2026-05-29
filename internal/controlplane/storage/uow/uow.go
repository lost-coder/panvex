// internal/controlplane/storage/uow/uow.go
//
// UnitOfWork lets a Service open a single transaction and operate on
// multiple Repository instances bound to it. Each Repository call
// inside fn is part of the same Tx; on fn returning error, the Tx
// rolls back; on success, commits.
//
// Contract (mirrors storage.Store.Transact):
//   - On fn returning nil: Tx commits.
//   - On fn returning a non-nil error: Tx rolls back, error propagates.
//   - On panic inside fn: Tx rolls back, panic re-raised.
//   - On ctx cancellation during fn: Tx aborts.
//   - Postgres: serialization failures (SQLSTATE 40001) retried up to 3 times.
//   - SQLite: BEGIN IMMEDIATE; no retry.
//   - Nested Do calls return storage.ErrNestedTransact.
package uow

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// UnitOfWork opens a single database transaction and exposes the domain
// repositories bound to it. Implementations are in the storage/postgres
// and storage/sqlite sub-packages.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(rs RepoSet) error) error
}

// RepoSet surfaces the domain repositories that participate in a single
// UnitOfWork transaction. All method calls on the returned repositories
// are part of the same transaction.
//
// Audit is intentionally absent: audit is a cross-cutting concern owned
// by the single serialized server-side hash-chainer, not a transactional
// domain repository. Writing audit through the UoW bypassed the chainer
// and broke the tamper-evident chain (C-1b).
type RepoSet interface {
	Clients() clients.Repository
	Discovered() discovered.Repository
	Jobs() jobs.Repository
}
