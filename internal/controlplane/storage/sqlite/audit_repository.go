// internal/controlplane/storage/sqlite/audit_repository.go
//
// audit.Repository implementation backed by SQLite via direct database/sql
// queries. Mirrors the Postgres implementation (storage/postgres/audit_repository.go)
// but uses ? placeholders and SQLite-specific type handling (INTEGER unix
// timestamps, JSON-as-TEXT).
package sqlite

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/audit"
)

// auditRepository implements audit.Repository against SQLite.
// db satisfies dbtx which is implemented by both *sql.DB and *sql.Tx,
// enabling the same code to run inside or outside a transaction.
type auditRepository struct {
	db dbtx
}

// NewAuditRepository wires an audit.Repository against a SQLite
// connection or transaction. Accepts *sql.DB (pool) or *sql.Tx.
// When called with a *Store, use store.DB() to pass the underlying *sql.DB.
func NewAuditRepository(db dbtx) audit.Repository {
	return &auditRepository{db: db}
}

func (r *auditRepository) Append(ctx context.Context, e audit.Event) error {
	detailsJSON, err := encodeJSON(e.Details)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO audit_events (id, actor_id, action, target_id, created_at_unix, details, prev_hash, event_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.ActorID, e.Action, e.TargetID, toUnix(e.CreatedAt), detailsJSON, e.PrevHash, e.EventHash)
	return err
}
