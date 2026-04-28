package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

func (s *Store) PutJob(ctx context.Context, job storage.JobRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE
		SET action = EXCLUDED.action,
		    idempotency_key = EXCLUDED.idempotency_key,
		    actor_id = EXCLUDED.actor_id,
		    status = EXCLUDED.status,
		    created_at = EXCLUDED.created_at,
		    ttl_nanos = EXCLUDED.ttl_nanos,
		    payload_json = EXCLUDED.payload_json
	`, job.ID, job.Action, job.IdempotencyKey, job.ActorID, job.Status, job.CreatedAt.UTC(), job.TTL.Nanoseconds(), job.PayloadJSON)
	return err
}

func (s *Store) GetJobByIdempotencyKey(ctx context.Context, idempotencyKey string) (storage.JobRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json
		FROM jobs
		WHERE idempotency_key = $1
	`, idempotencyKey)

	var job storage.JobRecord
	var ttlNanos int64
	if err := row.Scan(&job.ID, &job.Action, &job.IdempotencyKey, &job.ActorID, &job.Status, &job.CreatedAt, &ttlNanos, &job.PayloadJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.JobRecord{}, storage.ErrNotFound
		}
		return storage.JobRecord{}, err
	}

	job.CreatedAt = job.CreatedAt.UTC()
	job.TTL = time.Duration(ttlNanos)
	return job, nil
}

// ListJobs returns every job ordered by created_at + id for stable
// pagination. Phase-3 §3.1 (continued): wired through dbsqlc.ListJobs;
// the SQL definition in db/queries/jobs.sql is the single source of
// truth for column set + ORDER BY.
func (s *Store) ListJobs(ctx context.Context) ([]storage.JobRecord, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	rows, err := dbsqlc.New(s.sqlDB).ListJobs(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]storage.JobRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, jobRecordFromRow(row))
	}
	return result, nil
}

// jobRecordFromRow bridges the sqlc-emitted Job to the domain
// storage.JobRecord. The only non-trivial field is TTL: the column is
// stored as nanoseconds (int64) but the domain model wants time.Duration.
func jobRecordFromRow(row dbsqlc.Job) storage.JobRecord {
	return storage.JobRecord{
		ID:             row.ID,
		Action:         row.Action,
		ActorID:        row.ActorID,
		Status:         row.Status,
		CreatedAt:      row.CreatedAt.UTC(),
		TTL:            time.Duration(row.TtlNanos),
		IdempotencyKey: row.IdempotencyKey,
		PayloadJSON:    row.PayloadJson,
	}
}

func (s *Store) PutJobTarget(ctx context.Context, target storage.JobTargetRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO job_targets (job_id, agent_id, status, result_text, result_json, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (job_id, agent_id) DO UPDATE
		SET status = EXCLUDED.status,
		    result_text = EXCLUDED.result_text,
		    result_json = EXCLUDED.result_json,
		    updated_at = EXCLUDED.updated_at
	`, target.JobID, target.AgentID, target.Status, target.ResultText, target.ResultJSON, target.UpdatedAt.UTC())
	return err
}

// ListJobTargets returns every delivery row for one job, ordered by
// agent_id. Wired through dbsqlc.ListJobTargets.
func (s *Store) ListJobTargets(ctx context.Context, jobID string) ([]storage.JobTargetRecord, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	rows, err := dbsqlc.New(s.sqlDB).ListJobTargets(ctx, jobID)
	if err != nil {
		return nil, err
	}
	result := make([]storage.JobTargetRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, storage.JobTargetRecord{
			JobID:      row.JobID,
			AgentID:    row.AgentID,
			Status:     row.Status,
			ResultText: row.ResultText,
			ResultJSON: row.ResultJson,
			UpdatedAt:  row.UpdatedAt.UTC(),
		})
	}
	return result, nil
}

// ListAllJobTargets returns every job_targets row in one round-trip so
// the service-level restore loop can hydrate Job.Targets without per-job
// N+1 SELECTs.
func (s *Store) ListAllJobTargets(ctx context.Context) ([]storage.JobTargetRecord, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT job_id, agent_id, status, result_text, result_json, updated_at
		FROM job_targets
		ORDER BY job_id, agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]storage.JobTargetRecord, 0)
	for rows.Next() {
		var t storage.JobTargetRecord
		if err := rows.Scan(&t.JobID, &t.AgentID, &t.Status, &t.ResultText, &t.ResultJSON, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.UpdatedAt = t.UpdatedAt.UTC()
		result = append(result, t)
	}
	return result, rows.Err()
}

// PruneTerminalJobs deletes jobs in a finished status whose created_at
// predates the cutoff (Q2.U-P-02). job_targets is cleaned up via ON
// DELETE CASCADE in the schema.
func (s *Store) PruneTerminalJobs(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM jobs
		WHERE status IN ('succeeded','failed','expired')
		  AND created_at < $1
	`, before.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
