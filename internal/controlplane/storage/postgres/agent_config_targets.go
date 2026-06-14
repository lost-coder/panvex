package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// GetAgentConfigTarget returns the operator-desired Telemt config for one scope.
// Returns storage.ErrNotFound when no row exists for the given scopeType+scopeID pair.
func (s *Store) GetAgentConfigTarget(ctx context.Context, scopeType, scopeID string) (storage.AgentConfigTargetRecord, error) {
	if s.sqlDB == nil {
		return storage.AgentConfigTargetRecord{}, errTxBoundStore
	}
	row, err := dbsqlc.New(s.sqlDB).GetAgentConfigTarget(ctx, dbsqlc.GetAgentConfigTargetParams{
		ScopeType: scopeType,
		ScopeID:   scopeID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.AgentConfigTargetRecord{}, storage.ErrNotFound
		}
		return storage.AgentConfigTargetRecord{}, err
	}
	return storage.AgentConfigTargetRecord{
		ScopeType:    row.ScopeType,
		ScopeID:      row.ScopeID,
		SectionsJSON: row.SectionsJson,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

// ListAgentConfigTargets returns all operator-desired Telemt config targets,
// ordered by scope_type ASC, scope_id ASC.
func (s *Store) ListAgentConfigTargets(ctx context.Context) ([]storage.AgentConfigTargetRecord, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	rows, err := dbsqlc.New(s.sqlDB).ListAgentConfigTargets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]storage.AgentConfigTargetRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.AgentConfigTargetRecord{
			ScopeType:    row.ScopeType,
			ScopeID:      row.ScopeID,
			SectionsJSON: row.SectionsJson,
			CreatedAt:    row.CreatedAt,
			UpdatedAt:    row.UpdatedAt,
		})
	}
	return out, nil
}

// UpsertAgentConfigTarget inserts or updates the operator-desired Telemt config
// for one scope. On conflict (scope_type, scope_id) the sections_json and
// updated_at are overwritten.
func (s *Store) UpsertAgentConfigTarget(ctx context.Context, rec storage.AgentConfigTargetRecord) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).UpsertAgentConfigTarget(ctx, dbsqlc.UpsertAgentConfigTargetParams{
		ScopeType:    rec.ScopeType,
		ScopeID:      rec.ScopeID,
		SectionsJson: rec.SectionsJSON,
		CreatedAt:    rec.CreatedAt,
		UpdatedAt:    rec.UpdatedAt,
	})
}

// DeleteAgentConfigTarget removes the config target row for one scope.
// Returns the number of rows deleted (0 if the row did not exist).
func (s *Store) DeleteAgentConfigTarget(ctx context.Context, scopeType, scopeID string) (int64, error) {
	if s.sqlDB == nil {
		return 0, errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).DeleteAgentConfigTarget(ctx, dbsqlc.DeleteAgentConfigTargetParams{
		ScopeType: scopeType,
		ScopeID:   scopeID,
	})
}
