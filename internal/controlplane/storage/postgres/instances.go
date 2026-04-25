package postgres

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutInstance(ctx context.Context, instance storage.InstanceRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_instances (id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE
		SET agent_id = EXCLUDED.agent_id,
		    name = EXCLUDED.name,
		    version = EXCLUDED.version,
		    config_fingerprint = EXCLUDED.config_fingerprint,
		    connected_users = EXCLUDED.connected_users,
		    read_only = EXCLUDED.read_only,
		    updated_at = EXCLUDED.updated_at
	`, instance.ID, instance.AgentID, instance.Name, instance.Version, instance.ConfigFingerprint, instance.ConnectedUsers, instance.ReadOnly, instance.UpdatedAt.UTC())
	return err
}

func (s *Store) ListInstances(ctx context.Context) ([]storage.InstanceRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at
		FROM telemt_instances
		ORDER BY updated_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.InstanceRecord, 0)
	for rows.Next() {
		var instance storage.InstanceRecord
		if err := rows.Scan(&instance.ID, &instance.AgentID, &instance.Name, &instance.Version, &instance.ConfigFingerprint, &instance.ConnectedUsers, &instance.ReadOnly, &instance.UpdatedAt); err != nil {
			return nil, err
		}
		instance.UpdatedAt = instance.UpdatedAt.UTC()
		result = append(result, instance)
	}

	return result, rows.Err()
}
