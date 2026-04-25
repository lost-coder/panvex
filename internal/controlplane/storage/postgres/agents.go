package postgres

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

	var certIssuedAt sql.NullTime
	if agent.CertIssuedAt != nil {
		certIssuedAt.Valid = true
		certIssuedAt.Time = agent.CertIssuedAt.UTC()
	}
	var certExpiresAt sql.NullTime
	if agent.CertExpiresAt != nil {
		certExpiresAt.Valid = true
		certExpiresAt.Time = agent.CertExpiresAt.UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (id, node_name, fleet_group_id, version, read_only, last_seen_at, cert_issued_at, cert_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE
		SET node_name = EXCLUDED.node_name,
		    fleet_group_id = EXCLUDED.fleet_group_id,
		    version = EXCLUDED.version,
		    read_only = EXCLUDED.read_only,
		    last_seen_at = EXCLUDED.last_seen_at,
		    cert_issued_at = EXCLUDED.cert_issued_at,
		    cert_expires_at = EXCLUDED.cert_expires_at
	`, agent.ID, agent.NodeName, fleetGroupID, agent.Version, agent.ReadOnly, agent.LastSeenAt.UTC(), certIssuedAt, certExpiresAt)
	return err
}

func (s *Store) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_name, fleet_group_id, version, read_only, last_seen_at, cert_issued_at, cert_expires_at
		FROM agents
		ORDER BY last_seen_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AgentRecord, 0)
	for rows.Next() {
		var agent storage.AgentRecord
		var fleetGroupID sql.NullString
		var certIssuedAt sql.NullTime
		var certExpiresAt sql.NullTime
		if err := rows.Scan(&agent.ID, &agent.NodeName, &fleetGroupID, &agent.Version, &agent.ReadOnly, &agent.LastSeenAt, &certIssuedAt, &certExpiresAt); err != nil {
			return nil, err
		}
		if fleetGroupID.Valid {
			agent.FleetGroupID = fleetGroupID.String
		}
		agent.LastSeenAt = agent.LastSeenAt.UTC()
		if certIssuedAt.Valid {
			t := certIssuedAt.Time.UTC()
			agent.CertIssuedAt = &t
		}
		if certExpiresAt.Valid {
			t := certExpiresAt.Time.UTC()
			agent.CertExpiresAt = &t
		}
		result = append(result, agent)
	}

	return result, rows.Err()
}

func (s *Store) DeleteAgent(ctx context.Context, agentID string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM agents
		WHERE id = $1
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
		SET node_name = $2
		WHERE id = $1
	`, agentID, nodeName)
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
		WHERE agent_id = $1
	`, agentID)
	return err
}
