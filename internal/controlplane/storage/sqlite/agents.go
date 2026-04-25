package sqlite

import (
	"context"
	"database/sql"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutAgent(ctx context.Context, agent storage.AgentRecord) error {
	var fleetGroupID sql.NullString
	if agent.FleetGroupID != "" {
		fleetGroupID.Valid = true
		fleetGroupID.String = agent.FleetGroupID
	}

	var certIssuedAtUnix sql.NullInt64
	if agent.CertIssuedAt != nil {
		certIssuedAtUnix.Valid = true
		certIssuedAtUnix.Int64 = agent.CertIssuedAt.UTC().Unix()
	}
	var certExpiresAtUnix sql.NullInt64
	if agent.CertExpiresAt != nil {
		certExpiresAtUnix.Valid = true
		certExpiresAtUnix.Int64 = agent.CertExpiresAt.UTC().Unix()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (id, node_name, fleet_group_id, version, read_only, last_seen_at_unix, cert_issued_at_unix, cert_expires_at_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			node_name = excluded.node_name,
			fleet_group_id = excluded.fleet_group_id,
			version = excluded.version,
			read_only = excluded.read_only,
			last_seen_at_unix = excluded.last_seen_at_unix,
			cert_issued_at_unix = excluded.cert_issued_at_unix,
			cert_expires_at_unix = excluded.cert_expires_at_unix
	`, agent.ID, agent.NodeName, fleetGroupID, agent.Version, boolToInt(agent.ReadOnly), toUnix(agent.LastSeenAt), certIssuedAtUnix, certExpiresAtUnix)
	return err
}

func (s *Store) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_name, fleet_group_id, version, read_only, last_seen_at_unix, cert_issued_at_unix, cert_expires_at_unix
		FROM agents
		ORDER BY last_seen_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AgentRecord, 0)
	for rows.Next() {
		var agent storage.AgentRecord
		var fleetGroupID sql.NullString
		var readOnly int
		var lastSeenAt int64
		var certIssuedAtUnix sql.NullInt64
		var certExpiresAtUnix sql.NullInt64
		if err := rows.Scan(&agent.ID, &agent.NodeName, &fleetGroupID, &agent.Version, &readOnly, &lastSeenAt, &certIssuedAtUnix, &certExpiresAtUnix); err != nil {
			return nil, err
		}
		if fleetGroupID.Valid {
			agent.FleetGroupID = fleetGroupID.String
		}
		agent.ReadOnly = intToBool(readOnly)
		agent.LastSeenAt = fromUnix(lastSeenAt)
		if certIssuedAtUnix.Valid {
			t := fromUnix(certIssuedAtUnix.Int64)
			agent.CertIssuedAt = &t
		}
		if certExpiresAtUnix.Valid {
			t := fromUnix(certExpiresAtUnix.Int64)
			agent.CertExpiresAt = &t
		}
		result = append(result, agent)
	}

	return result, rows.Err()
}

func (s *Store) DeleteAgent(ctx context.Context, agentID string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM agents
		WHERE id = ?
	`, agentID)
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

func (s *Store) UpdateAgentNodeName(ctx context.Context, agentID string, nodeName string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agents
		SET node_name = ?
		WHERE id = ?
	`, nodeName, agentID)
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

func (s *Store) DeleteInstancesByAgent(ctx context.Context, agentID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM instances
		WHERE agent_id = ?
	`, agentID)
	return err
}
