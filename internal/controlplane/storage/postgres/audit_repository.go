// internal/controlplane/storage/postgres/audit_repository.go
//
// audit.Repository implementation backed by Postgres via dbsqlc.
package postgres

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/audit"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// auditRepository implements audit.Repository against Postgres via
// the dbsqlc query layer.
type auditRepository struct {
	q *dbsqlc.Queries
}

// NewAuditRepository wires an audit.Repository against a Postgres
// connection or transaction. db may be *sql.DB (pool) or *sql.Tx.
func NewAuditRepository(db dbsqlc.DBTX) audit.Repository {
	return &auditRepository{q: dbsqlc.New(db)}
}

func (r *auditRepository) Append(ctx context.Context, e audit.Event) error {
	detailsJSON, err := encodeJSON(e.Details)
	if err != nil {
		return err
	}
	return r.q.AppendAuditEvent(ctx, dbsqlc.AppendAuditEventParams{
		ID:        e.ID,
		ActorID:   e.ActorID,
		Action:    e.Action,
		TargetID:  e.TargetID,
		Details:   detailsJSON,
		CreatedAt: e.CreatedAt.UTC(),
		PrevHash:  e.PrevHash,
		EventHash: e.EventHash,
	})
}
