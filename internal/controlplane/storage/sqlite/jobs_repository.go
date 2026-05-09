// internal/controlplane/storage/sqlite/jobs_repository.go
//
// jobs.Repository implementation backed by SQLite via direct database/sql
// queries. Mirrors the Postgres implementation (storage/postgres/jobs_repository.go)
// but uses ? placeholders and SQLite-specific type handling (INTEGER unix
// timestamps for created_at_unix).
package sqlite

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// jobsRepository implements jobs.Repository against SQLite.
// db satisfies dbtx which is implemented by both *sql.DB and *sql.Tx,
// enabling the same code to run inside or outside a transaction.
type jobsRepository struct {
	db dbtx
}

// NewJobsRepository wires a jobs.Repository against a SQLite connection
// or transaction. Accepts *sql.DB (pool) or *sql.Tx.
// When called with a *Store, use store.DB() to pass the underlying *sql.DB.
func NewJobsRepository(db dbtx) jobs.Repository {
	return &jobsRepository{db: db}
}

func (r *jobsRepository) Put(ctx context.Context, j jobs.Job) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO jobs (id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			action          = excluded.action,
			actor_id        = excluded.actor_id,
			status          = excluded.status,
			created_at_unix = excluded.created_at_unix,
			ttl_nanos       = excluded.ttl_nanos,
			idempotency_key = excluded.idempotency_key,
			payload_json    = excluded.payload_json
	`, j.ID, string(j.Action), j.ActorID, string(j.Status),
		toUnix(j.CreatedAt), j.TTL.Nanoseconds(), j.IdempotencyKey, j.PayloadJSON)
	return err
}
