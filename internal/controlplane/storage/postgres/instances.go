package postgres

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03: routed through dbsqlc.

func (s *Store) PutInstance(ctx context.Context, instance storage.InstanceRecord) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).UpsertInstance(ctx, dbsqlc.UpsertInstanceParams{
		ID:                instance.ID,
		AgentID:           instance.AgentID,
		Name:              instance.Name,
		Version:           instance.Version,
		ConfigFingerprint: instance.ConfigFingerprint,
		Connections:       int64(instance.Connections),
		ReadOnly:          instance.ReadOnly,
		UpdatedAt:         instance.UpdatedAt.UTC(),
	})
}

func (s *Store) ListInstances(ctx context.Context) ([]storage.InstanceRecord, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	rows, err := dbsqlc.New(s.sqlDB).ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]storage.InstanceRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.InstanceRecord{
			ID:                row.ID,
			AgentID:           row.AgentID,
			Name:              row.Name,
			Version:           row.Version,
			ConfigFingerprint: row.ConfigFingerprint,
			Connections:       int(row.Connections),
			ReadOnly:          row.ReadOnly,
			UpdatedAt:         row.UpdatedAt.UTC(),
		})
	}
	return out, nil
}
