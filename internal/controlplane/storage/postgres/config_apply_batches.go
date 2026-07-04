package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// CreateConfigApplyBatch inserts a batch row and its full target set inside
// a single transaction via the store's internal-tx helper (beginInternalTx):
// either every row lands or none does.
func (s *Store) CreateConfigApplyBatch(ctx context.Context, b storage.ConfigApplyBatchRecord, targets []storage.ConfigApplyBatchTargetRecord) error {
	// P3-3.4: "" fleet group id = agent-scoped batch-of-one → SQL NULL.
	var fleetGroupID uuid.NullUUID
	if b.FleetGroupID != "" {
		parsed, err := uuid.Parse(b.FleetGroupID)
		if err != nil {
			return err
		}
		fleetGroupID = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	q := dbsqlc.New(tx)
	if err := q.InsertConfigApplyBatch(ctx, dbsqlc.InsertConfigApplyBatchParams{
		ID:               b.ID,
		FleetGroupID:     fleetGroupID,
		Mode:             b.Mode,
		WaveSize:         int32(b.WaveSize), //nolint:gosec // wave size is a small operator-supplied count, never near int32 overflow
		ExpectedRevision: b.ExpectedRevision,
		Status:           b.Status,
		CreatedAt:        b.CreatedAt.UTC(),
		UpdatedAt:        b.UpdatedAt.UTC(),
	}); err != nil {
		return err
	}

	for _, target := range targets {
		if err := q.InsertConfigApplyBatchTarget(ctx, dbsqlc.InsertConfigApplyBatchTargetParams{
			BatchID:   target.BatchID,
			AgentID:   target.AgentID,
			WaveIndex: int32(target.WaveIndex), //nolint:gosec // wave index is a small ordinal, never near int32 overflow
			JobID:     target.JobID,
			Status:    target.Status,
			Message:   target.Message,
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetConfigApplyBatch returns the batch plus every target row, ordered by
// wave_index then agent_id. Returns storage.ErrNotFound when no batch with
// the given id exists.
func (s *Store) GetConfigApplyBatch(ctx context.Context, id string) (storage.ConfigApplyBatchRecord, []storage.ConfigApplyBatchTargetRecord, error) {
	q := dbsqlc.New(s.db)

	row, err := q.GetConfigApplyBatch(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.ConfigApplyBatchRecord{}, nil, storage.ErrNotFound
		}
		return storage.ConfigApplyBatchRecord{}, nil, err
	}

	targetRows, err := q.ListConfigApplyBatchTargets(ctx, id)
	if err != nil {
		return storage.ConfigApplyBatchRecord{}, nil, err
	}
	targets := make([]storage.ConfigApplyBatchTargetRecord, 0, len(targetRows))
	for _, t := range targetRows {
		targets = append(targets, configApplyBatchTargetFromRow(t))
	}

	return storage.ConfigApplyBatchRecord{
		ID:               row.ID,
		FleetGroupID:     row.FleetGroupID,
		Mode:             row.Mode,
		WaveSize:         int(row.WaveSize),
		ExpectedRevision: row.ExpectedRevision,
		Status:           row.Status,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}, targets, nil
}

// ListRunningConfigApplyBatches returns every batch in
// storage.ConfigApplyBatchStatusRunning, ordered by created_at then id.
func (s *Store) ListRunningConfigApplyBatches(ctx context.Context) ([]storage.ConfigApplyBatchRecord, error) {
	rows, err := dbsqlc.New(s.db).ListRunningConfigApplyBatches(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]storage.ConfigApplyBatchRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.ConfigApplyBatchRecord{
			ID:               row.ID,
			FleetGroupID:     row.FleetGroupID,
			Mode:             row.Mode,
			WaveSize:         int(row.WaveSize),
			ExpectedRevision: row.ExpectedRevision,
			Status:           row.Status,
			CreatedAt:        row.CreatedAt.UTC(),
			UpdatedAt:        row.UpdatedAt.UTC(),
		})
	}
	return out, nil
}

// ActiveConfigApplyBatchForGroup returns the running batch for a fleet
// group, if any. The bool is false (with a zero-value record) when the
// group has no batch in storage.ConfigApplyBatchStatusRunning.
func (s *Store) ActiveConfigApplyBatchForGroup(ctx context.Context, fleetGroupID string) (storage.ConfigApplyBatchRecord, bool, error) {
	parsed, err := uuid.Parse(fleetGroupID)
	if err != nil {
		return storage.ConfigApplyBatchRecord{}, false, err
	}
	// The lookup always targets a real group; NULL (agent-scoped) batches
	// never match `WHERE fleet_group_id = $1` (P3-3.4).
	row, err := dbsqlc.New(s.db).GetActiveConfigApplyBatchForGroup(ctx, uuid.NullUUID{UUID: parsed, Valid: true})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.ConfigApplyBatchRecord{}, false, nil
		}
		return storage.ConfigApplyBatchRecord{}, false, err
	}
	return storage.ConfigApplyBatchRecord{
		ID:               row.ID,
		FleetGroupID:     row.FleetGroupID,
		Mode:             row.Mode,
		WaveSize:         int(row.WaveSize),
		ExpectedRevision: row.ExpectedRevision,
		Status:           row.Status,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}, true, nil
}

// UpdateConfigApplyBatchStatus transitions a batch's status and bumps
// updated_at. Returns storage.ErrNotFound when no batch with the given id
// exists.
func (s *Store) UpdateConfigApplyBatchStatus(ctx context.Context, id, status string, now time.Time) error {
	rowsAffected, err := dbsqlc.New(s.db).UpdateConfigApplyBatchStatus(ctx, dbsqlc.UpdateConfigApplyBatchStatusParams{
		Status:    status,
		UpdatedAt: now.UTC(),
		ID:        id,
	})
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
	return dbsqlc.New(s.db).SetConfigApplyBatchTargetJob(ctx, dbsqlc.SetConfigApplyBatchTargetJobParams{
		JobID:   jobID,
		Status:  status,
		BatchID: batchID,
		AgentID: agentID,
	})
}

// UpdateConfigApplyBatchTargetStatus updates one target's delivery status
// and message without touching its job id.
func (s *Store) UpdateConfigApplyBatchTargetStatus(ctx context.Context, batchID, agentID, status, message string) error {
	return dbsqlc.New(s.db).UpdateConfigApplyBatchTargetStatus(ctx, dbsqlc.UpdateConfigApplyBatchTargetStatusParams{
		Status:  status,
		Message: message,
		BatchID: batchID,
		AgentID: agentID,
	})
}

// PruneConfigApplyBatches deletes batches in a terminal status
// (succeeded/failed/halted) whose updated_at predates before. Targets are
// removed via ON DELETE CASCADE.
func (s *Store) PruneConfigApplyBatches(ctx context.Context, before time.Time) (int64, error) {
	return dbsqlc.New(s.db).DeleteTerminalConfigApplyBatches(ctx, before.UTC())
}

// configApplyBatchTargetFromRow bridges the sqlc-emitted
// ConfigApplyBatchTarget to the domain storage.ConfigApplyBatchTargetRecord.
func configApplyBatchTargetFromRow(row dbsqlc.ConfigApplyBatchTarget) storage.ConfigApplyBatchTargetRecord {
	return storage.ConfigApplyBatchTargetRecord{
		BatchID:   row.BatchID,
		AgentID:   row.AgentID,
		WaveIndex: int(row.WaveIndex),
		JobID:     row.JobID,
		Status:    row.Status,
		Message:   row.Message,
	}
}
