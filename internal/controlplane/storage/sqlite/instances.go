package sqlite

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutInstance(ctx context.Context, instance storage.InstanceRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_instances (id, agent_id, name, version, config_fingerprint, connections, read_only, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			agent_id = excluded.agent_id,
			name = excluded.name,
			version = excluded.version,
			config_fingerprint = excluded.config_fingerprint,
			connections = excluded.connections,
			read_only = excluded.read_only,
			updated_at_unix = excluded.updated_at_unix
	`, instance.ID, instance.AgentID, instance.Name, instance.Version, instance.ConfigFingerprint, instance.Connections, boolToInt(instance.ReadOnly), toUnix(instance.UpdatedAt))
	return err
}

func (s *Store) ListInstances(ctx context.Context) ([]storage.InstanceRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, name, version, config_fingerprint, connections, read_only, updated_at_unix
		FROM telemt_instances
		ORDER BY updated_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.InstanceRecord, 0)
	for rows.Next() {
		var instance storage.InstanceRecord
		var readOnly int
		var updatedAt int64
		if err := rows.Scan(&instance.ID, &instance.AgentID, &instance.Name, &instance.Version, &instance.ConfigFingerprint, &instance.Connections, &readOnly, &updatedAt); err != nil {
			return nil, err
		}
		instance.ReadOnly = intToBool(readOnly)
		instance.UpdatedAt = fromUnix(updatedAt)
		result = append(result, instance)
	}

	return result, rows.Err()
}
