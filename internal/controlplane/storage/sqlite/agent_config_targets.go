package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// GetAgentConfigTarget returns the operator-desired Telemt config for one scope.
// Returns storage.ErrNotFound when no row exists for the given scopeType+scopeID pair.
func (s *Store) GetAgentConfigTarget(ctx context.Context, scopeType, scopeID string) (storage.AgentConfigTargetRecord, error) {
	const q = `SELECT scope_type, scope_id, sections_json, created_at, updated_at
		FROM agent_config_targets WHERE scope_type = ? AND scope_id = ?`
	var rec storage.AgentConfigTargetRecord
	err := s.db.QueryRowContext(ctx, q, scopeType, scopeID).
		Scan(&rec.ScopeType, &rec.ScopeID, &rec.SectionsJSON, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.AgentConfigTargetRecord{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.AgentConfigTargetRecord{}, err
	}
	return rec, nil
}

// ListAgentConfigTargets returns all operator-desired Telemt config targets,
// ordered by scope_type ASC, scope_id ASC.
func (s *Store) ListAgentConfigTargets(ctx context.Context) ([]storage.AgentConfigTargetRecord, error) {
	const q = `SELECT scope_type, scope_id, sections_json, created_at, updated_at
		FROM agent_config_targets ORDER BY scope_type ASC, scope_id ASC`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storage.AgentConfigTargetRecord
	for rows.Next() {
		var rec storage.AgentConfigTargetRecord
		if err := rows.Scan(&rec.ScopeType, &rec.ScopeID, &rec.SectionsJSON, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// UpsertAgentConfigTarget inserts or updates the operator-desired Telemt config
// for one scope. On conflict (scope_type, scope_id) the sections_json and
// updated_at are overwritten.
func (s *Store) UpsertAgentConfigTarget(ctx context.Context, rec storage.AgentConfigTargetRecord) error {
	const q = `INSERT INTO agent_config_targets (scope_type, scope_id, sections_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(scope_type, scope_id) DO UPDATE
		SET sections_json = excluded.sections_json, updated_at = excluded.updated_at`
	_, err := s.db.ExecContext(ctx, q, rec.ScopeType, rec.ScopeID, rec.SectionsJSON, rec.CreatedAt, rec.UpdatedAt)
	return err
}

// DeleteAgentConfigTarget removes the config target row for one scope.
// Returns the number of rows deleted (0 if the row did not exist).
func (s *Store) DeleteAgentConfigTarget(ctx context.Context, scopeType, scopeID string) (int64, error) {
	const q = `DELETE FROM agent_config_targets WHERE scope_type = ? AND scope_id = ?`
	res, err := s.db.ExecContext(ctx, q, scopeType, scopeID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
