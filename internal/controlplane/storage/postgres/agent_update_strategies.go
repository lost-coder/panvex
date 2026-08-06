package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// GetAgentUpdateStrategy returns the persisted Telemt update strategy for
// one agent. Returns storage.ErrNotFound when no row exists for the agent.
func (s *Store) GetAgentUpdateStrategy(ctx context.Context, agentID string) (storage.AgentUpdateStrategyRecord, error) {
	const q = `SELECT agent_id, mode, restart_spec, binary_path, asset_flavor, created_at, updated_at
		FROM agent_update_strategies WHERE agent_id = $1`
	var rec storage.AgentUpdateStrategyRecord
	err := s.db.QueryRowContext(ctx, q, agentID).
		Scan(&rec.AgentID, &rec.Mode, &rec.RestartSpec, &rec.BinaryPath, &rec.AssetFlavor, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.AgentUpdateStrategyRecord{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.AgentUpdateStrategyRecord{}, err
	}
	return rec, nil
}

// UpsertAgentUpdateStrategy inserts or replaces the update strategy for an
// agent. On conflict (agent_id) the mode/restart_spec/binary_path/
// asset_flavor/updated_at are overwritten; created_at is left untouched.
func (s *Store) UpsertAgentUpdateStrategy(ctx context.Context, rec storage.AgentUpdateStrategyRecord) error {
	const q = `INSERT INTO agent_update_strategies (agent_id, mode, restart_spec, binary_path, asset_flavor, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (agent_id) DO UPDATE
		SET mode = excluded.mode, restart_spec = excluded.restart_spec,
			binary_path = excluded.binary_path, asset_flavor = excluded.asset_flavor,
			updated_at = excluded.updated_at`
	_, err := s.db.ExecContext(ctx, q, rec.AgentID, rec.Mode, rec.RestartSpec, rec.BinaryPath, rec.AssetFlavor, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// DeleteAgentUpdateStrategy removes the update strategy row for an agent.
// Idempotent: deleting an absent row is not an error.
func (s *Store) DeleteAgentUpdateStrategy(ctx context.Context, agentID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_update_strategies WHERE agent_id = $1`, agentID)
	return err
}
