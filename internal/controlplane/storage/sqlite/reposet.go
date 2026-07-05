// internal/controlplane/storage/sqlite/reposet.go
//
// txRepoSet wires the domain repositories to a single dbtx
// (either *sql.Tx or connExecutor) so all Repository calls inside a
// UnitOfWork.Do belong to the same transaction.
package sqlite

import (
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/storage/uow"
)

// txRepoSet implements uow.RepoSet with all repositories bound to
// the same dbtx executor (connExecutor or *sql.Tx).
type txRepoSet struct {
	db dbtx
}

func newTxRepoSet(db dbtx) *txRepoSet {
	return &txRepoSet{db: db}
}

// Ensure txRepoSet satisfies the interface at compile time.
var _ uow.RepoSet = (*txRepoSet)(nil)

func (r *txRepoSet) Clients() clients.Repository {
	// NewClientsRepository accepts dbtx; pass nil for raw so no bulk-tx
	// path is available inside a UoW transaction (callers use the UoW tx).
	return NewClientsRepository(r.db)
}

func (r *txRepoSet) Discovered() discovered.Repository { return NewDiscoveredRepository(r.db) }
