// internal/controlplane/storage/postgres/reposet.go
//
// txRepoSet wires the four domain repositories to a single *sql.Tx so
// that all Repository calls inside a UnitOfWork.Do belong to the same
// transaction.
package postgres

import (
	"database/sql"

	"github.com/lost-coder/panvex/internal/controlplane/audit"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage/uow"
)

// txRepoSet implements uow.RepoSet with all four repositories bound to
// the same *sql.Tx. *sql.Tx satisfies dbsqlc.DBTX (ExecContext,
// QueryContext, QueryRowContext, PrepareContext), so all existing
// New*Repository constructors accept it directly.
type txRepoSet struct {
	tx *sql.Tx
}

func newTxRepoSet(tx *sql.Tx) *txRepoSet {
	return &txRepoSet{tx: tx}
}

// Ensure txRepoSet satisfies the interface at compile time.
var _ uow.RepoSet = (*txRepoSet)(nil)

func (r *txRepoSet) Clients() clients.Repository       { return NewClientsRepository(r.tx) }
func (r *txRepoSet) Discovered() discovered.Repository { return NewDiscoveredRepository(r.tx) }
func (r *txRepoSet) Audit() audit.Repository           { return NewAuditRepository(r.tx) }
func (r *txRepoSet) Jobs() jobs.Repository             { return NewJobsRepository(r.tx) }
