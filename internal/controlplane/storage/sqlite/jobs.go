package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutJob(ctx context.Context, job storage.JobRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			action = excluded.action,
			actor_id = excluded.actor_id,
			status = excluded.status,
			created_at_unix = excluded.created_at_unix,
			ttl_nanos = excluded.ttl_nanos,
			idempotency_key = excluded.idempotency_key,
			payload_json = excluded.payload_json
	`, job.ID, job.Action, job.ActorID, job.Status, toUnix(job.CreatedAt), job.TTL.Nanoseconds(), job.IdempotencyKey, job.PayloadJSON)
	return err
}

func (s *Store) GetJobByIdempotencyKey(ctx context.Context, idempotencyKey string) (storage.JobRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json
		FROM jobs
		WHERE idempotency_key = ?
	`, idempotencyKey)

	var job storage.JobRecord
	var createdAt int64
	var ttlNanos int64
	if err := row.Scan(&job.ID, &job.Action, &job.ActorID, &job.Status, &createdAt, &ttlNanos, &job.IdempotencyKey, &job.PayloadJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.JobRecord{}, storage.ErrNotFound
		}
		return storage.JobRecord{}, err
	}

	job.CreatedAt = fromUnix(createdAt)
	job.TTL = time.Duration(ttlNanos)
	return job, nil
}

func (s *Store) ListJobs(ctx context.Context) ([]storage.JobRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json
		FROM jobs
		ORDER BY created_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.JobRecord, 0)
	for rows.Next() {
		var job storage.JobRecord
		var createdAt int64
		var ttlNanos int64
		if err := rows.Scan(&job.ID, &job.Action, &job.ActorID, &job.Status, &createdAt, &ttlNanos, &job.IdempotencyKey, &job.PayloadJSON); err != nil {
			return nil, err
		}
		job.CreatedAt = fromUnix(createdAt)
		job.TTL = time.Duration(ttlNanos)
		result = append(result, job)
	}

	return result, rows.Err()
}

func (s *Store) PutJobTarget(ctx context.Context, target storage.JobTargetRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO job_targets (job_id, agent_id, status, result_text, result_json, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id, agent_id) DO UPDATE SET
			status = excluded.status,
			result_text = excluded.result_text,
			result_json = excluded.result_json,
			updated_at_unix = excluded.updated_at_unix
	`, target.JobID, target.AgentID, target.Status, target.ResultText, target.ResultJSON, toUnix(target.UpdatedAt))
	return err
}

func (s *Store) ListJobTargets(ctx context.Context, jobID string) ([]storage.JobTargetRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id, agent_id, status, result_text, result_json, updated_at_unix
		FROM job_targets
		WHERE job_id = ?
		ORDER BY agent_id
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.JobTargetRecord, 0)
	for rows.Next() {
		var target storage.JobTargetRecord
		var updatedAt int64
		if err := rows.Scan(&target.JobID, &target.AgentID, &target.Status, &target.ResultText, &target.ResultJSON, &updatedAt); err != nil {
			return nil, err
		}
		target.UpdatedAt = fromUnix(updatedAt)
		result = append(result, target)
	}

	return result, rows.Err()
}

// ListAllJobTargets returns every job_targets row in one query so the
// service-level restore loop can fold targets into a map[jobID][]targets
// without N+1 SELECTs.
func (s *Store) ListAllJobTargets(ctx context.Context) ([]storage.JobTargetRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id, agent_id, status, result_text, result_json, updated_at_unix
		FROM job_targets
		ORDER BY job_id, agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.JobTargetRecord, 0)
	for rows.Next() {
		var target storage.JobTargetRecord
		var updatedAt int64
		if err := rows.Scan(&target.JobID, &target.AgentID, &target.Status, &target.ResultText, &target.ResultJSON, &updatedAt); err != nil {
			return nil, err
		}
		target.UpdatedAt = fromUnix(updatedAt)
		result = append(result, target)
	}

	return result, rows.Err()
}

// PruneTerminalJobs deletes jobs in a finished status whose
// created_at predates the cutoff (Q2.U-P-02). job_targets is cleaned
// up via ON DELETE CASCADE in the schema.
func (s *Store) PruneTerminalJobs(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM jobs
		WHERE status IN ('succeeded','failed','expired')
		  AND created_at_unix < ?
	`, toUnix(before))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
