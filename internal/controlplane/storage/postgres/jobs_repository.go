// internal/controlplane/storage/postgres/jobs_repository.go
//
// jobs.Repository implementation backed by Postgres via direct database/sql
// queries. Mirrors the inline SQL from (*Store).PutJob in jobs.go — kept
// here so the same INSERT can run against *sql.DB or *sql.Tx via the
// dbsqlc.DBTX interface.
package postgres

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// jobsRepository implements jobs.Repository against Postgres.
// db satisfies dbsqlc.DBTX which is implemented by both *sql.DB and
// *sql.Tx, enabling the same code to run inside or outside a transaction.
type jobsRepository struct {
	db dbsqlc.DBTX
}

// NewJobsRepository wires a jobs.Repository against a Postgres connection
// or transaction. db may be *sql.DB (pool) or *sql.Tx.
func NewJobsRepository(db dbsqlc.DBTX) jobs.Repository {
	return &jobsRepository{db: db}
}

func (r *jobsRepository) Put(ctx context.Context, j jobs.Job) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO jobs (id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE
		SET action          = EXCLUDED.action,
		    idempotency_key = EXCLUDED.idempotency_key,
		    actor_id        = EXCLUDED.actor_id,
		    status          = EXCLUDED.status,
		    created_at      = EXCLUDED.created_at,
		    ttl_nanos       = EXCLUDED.ttl_nanos,
		    payload_json    = EXCLUDED.payload_json
	`, j.ID, string(j.Action), j.IdempotencyKey, j.ActorID, string(j.Status),
		j.CreatedAt.UTC(), j.TTL.Nanoseconds(), j.PayloadJSON)
	return err
}
