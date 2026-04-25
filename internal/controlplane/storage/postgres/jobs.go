package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
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

func (s *Store) ListJobs(ctx context.Context) ([]storage.JobRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json
		FROM jobs
		ORDER BY created_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.JobRecord, 0)
	for rows.Next() {
		var job storage.JobRecord
		var ttlNanos int64
		if err := rows.Scan(&job.ID, &job.Action, &job.IdempotencyKey, &job.ActorID, &job.Status, &job.CreatedAt, &ttlNanos, &job.PayloadJSON); err != nil {
			return nil, err
		}
		job.CreatedAt = job.CreatedAt.UTC()
		job.TTL = time.Duration(ttlNanos)
		result = append(result, job)
	}

	return result, rows.Err()
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

func (s *Store) ListJobTargets(ctx context.Context, jobID string) ([]storage.JobTargetRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id, agent_id, status, result_text, result_json, updated_at
		FROM job_targets
		WHERE job_id = $1
		ORDER BY agent_id
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.JobTargetRecord, 0)
	for rows.Next() {
		var target storage.JobTargetRecord
		if err := rows.Scan(&target.JobID, &target.AgentID, &target.Status, &target.ResultText, &target.ResultJSON, &target.UpdatedAt); err != nil {
			return nil, err
		}
		target.UpdatedAt = target.UpdatedAt.UTC()
		result = append(result, target)
	}

	return result, rows.Err()
}
