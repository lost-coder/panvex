package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// CreateConfigApplyBatch inserts a batch row and its full target set inside
// a single transaction (beginInternalTx): either every row lands or none
// does. This is a multi-statement write — it MUST run inside a transaction
// (see storage.ConfigApplyBatchStore.CreateConfigApplyBatch).
func (s *Store) CreateConfigApplyBatch(ctx context.Context, b storage.ConfigApplyBatchRecord, targets []storage.ConfigApplyBatchTargetRecord) error {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO config_apply_batches
			(id, fleet_group_id, mode, wave_size, expected_revision, status, created_at_unix, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, b.ID, nullableString(b.FleetGroupID), b.Mode, b.WaveSize, b.ExpectedRevision, b.Status, toUnix(b.CreatedAt), toUnix(b.UpdatedAt)); err != nil {
		return err
	}

	for _, target := range targets {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO config_apply_batch_targets (batch_id, agent_id, wave_index, job_id, status, message)
			VALUES (?, ?, ?, ?, ?, ?)
		`, target.BatchID, target.AgentID, target.WaveIndex, target.JobID, target.Status, target.Message); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetConfigApplyBatch returns the batch plus every target row, ordered by
// wave_index then agent_id. Returns storage.ErrNotFound when no batch with
// the given id exists.
func (s *Store) GetConfigApplyBatch(ctx context.Context, id string) (storage.ConfigApplyBatchRecord, []storage.ConfigApplyBatchTargetRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, fleet_group_id, mode, wave_size, expected_revision, status, created_at_unix, updated_at_unix
		FROM config_apply_batches
		WHERE id = ?
	`, id)

	batch, err := scanConfigApplyBatch(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ConfigApplyBatchRecord{}, nil, storage.ErrNotFound
	}
	if err != nil {
		return storage.ConfigApplyBatchRecord{}, nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT batch_id, agent_id, wave_index, job_id, status, message
		FROM config_apply_batch_targets
		WHERE batch_id = ?
		ORDER BY wave_index ASC, agent_id ASC
	`, id)
	if err != nil {
		return storage.ConfigApplyBatchRecord{}, nil, err
	}
	defer rows.Close()

	targets := make([]storage.ConfigApplyBatchTargetRecord, 0)
	for rows.Next() {
		var t storage.ConfigApplyBatchTargetRecord
		if err := rows.Scan(&t.BatchID, &t.AgentID, &t.WaveIndex, &t.JobID, &t.Status, &t.Message); err != nil {
			return storage.ConfigApplyBatchRecord{}, nil, err
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return storage.ConfigApplyBatchRecord{}, nil, err
	}

	return batch, targets, nil
}

// ListRunningConfigApplyBatches returns every batch in
// storage.ConfigApplyBatchStatusRunning, ordered by created_at then id.
func (s *Store) ListRunningConfigApplyBatches(ctx context.Context) ([]storage.ConfigApplyBatchRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, fleet_group_id, mode, wave_size, expected_revision, status, created_at_unix, updated_at_unix
		FROM config_apply_batches
		WHERE status = ?
		ORDER BY created_at_unix ASC, id ASC
	`, storage.ConfigApplyBatchStatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]storage.ConfigApplyBatchRecord, 0)
	for rows.Next() {
		batch, err := scanConfigApplyBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, batch)
	}
	return out, rows.Err()
}

// ActiveConfigApplyBatchForGroup returns the running batch for a fleet
// group, if any. The bool is false (with a zero-value record) when the
// group has no batch in storage.ConfigApplyBatchStatusRunning.
func (s *Store) ActiveConfigApplyBatchForGroup(ctx context.Context, fleetGroupID string) (storage.ConfigApplyBatchRecord, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, fleet_group_id, mode, wave_size, expected_revision, status, created_at_unix, updated_at_unix
		FROM config_apply_batches
		WHERE fleet_group_id = ? AND status = ?
		ORDER BY created_at_unix ASC, id ASC
		LIMIT 1
	`, fleetGroupID, storage.ConfigApplyBatchStatusRunning)

	batch, err := scanConfigApplyBatch(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ConfigApplyBatchRecord{}, false, nil
	}
	if err != nil {
		return storage.ConfigApplyBatchRecord{}, false, err
	}
	return batch, true, nil
}

// UpdateConfigApplyBatchStatus transitions a batch's status and bumps
// updated_at. Returns storage.ErrNotFound when no batch with the given id
// exists.
func (s *Store) UpdateConfigApplyBatchStatus(ctx context.Context, id, status string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE config_apply_batches
		SET status = ?, updated_at_unix = ?
		WHERE id = ?
	`, status, toUnix(now), id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// SetConfigApplyBatchTargetJob records the job enqueued for one target (wave
// enqueue) and updates its status in the same write.
func (s *Store) SetConfigApplyBatchTargetJob(ctx context.Context, batchID, agentID, jobID, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE config_apply_batch_targets
		SET job_id = ?, status = ?
		WHERE batch_id = ? AND agent_id = ?
	`, jobID, status, batchID, agentID)
	return err
}

// UpdateConfigApplyBatchTargetStatus updates one target's delivery status
// and message without touching its job id.
func (s *Store) UpdateConfigApplyBatchTargetStatus(ctx context.Context, batchID, agentID, status, message string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE config_apply_batch_targets
		SET status = ?, message = ?
		WHERE batch_id = ? AND agent_id = ?
	`, status, message, batchID, agentID)
	return err
}

// PruneConfigApplyBatches deletes batches in a terminal status
// (succeeded/failed/halted) whose updated_at predates before. Targets are
// removed via ON DELETE CASCADE.
func (s *Store) PruneConfigApplyBatches(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM config_apply_batches
		WHERE status IN (?, ?, ?)
		  AND updated_at_unix < ?
	`, storage.ConfigApplyBatchStatusSucceeded, storage.ConfigApplyBatchStatusFailed, storage.ConfigApplyBatchStatusHalted, toUnix(before))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// nullableString отдаёт NULL вместо пустой строки — "" в
// ConfigApplyBatchRecord.FleetGroupID означает agent-scoped батч без
// fleet-group-скоупа (P3-3.4); NULL не участвует в FK-проверке.
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// scanConfigApplyBatch uses the shared rowScanner interface (webhooks.go) so
// it works for both QueryRowContext and QueryContext call sites.
func scanConfigApplyBatch(row rowScanner) (storage.ConfigApplyBatchRecord, error) {
	var b storage.ConfigApplyBatchRecord
	var createdAt, updatedAt int64
	var fleetGroupID sql.NullString
	if err := row.Scan(&b.ID, &fleetGroupID, &b.Mode, &b.WaveSize, &b.ExpectedRevision, &b.Status, &createdAt, &updatedAt); err != nil {
		return storage.ConfigApplyBatchRecord{}, err
	}
	b.FleetGroupID = fleetGroupID.String
	b.CreatedAt = fromUnix(createdAt)
	b.UpdatedAt = fromUnix(updatedAt)
	return b, nil
}
