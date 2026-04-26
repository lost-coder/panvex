package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
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

// ListAgents returns every agent the panel knows about, ordered by
// last_seen_at + id for stable pagination.
//
// Phase-3 §3.1: this is the first method to consume the sqlc-generated
// dbsqlc.Queries surface. Conversion from dbsqlc.ListAgentsRow to the
// storage.AgentRecord shape lives in agentRecordFromRow below; if a
// future query gets migrated, that helper stays the only place that
// knows about the SQL → domain mapping.
func (s *Store) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	rows, err := dbsqlc.New(s.sqlDB).ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]storage.AgentRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, agentRecordFromRow(row))
	}
	return result, nil
}

// agentRecordFromRow is the SQL-row → domain-DTO bridge for ListAgents.
// Kept private to the postgres package: callers see only storage.AgentRecord.
func agentRecordFromRow(row dbsqlc.ListAgentsRow) storage.AgentRecord {
	rec := storage.AgentRecord{
		ID:         row.ID,
		NodeName:   row.NodeName,
		Version:    row.Version,
		ReadOnly:   row.ReadOnly,
		LastSeenAt: row.LastSeenAt.UTC(),
	}
	if row.FleetGroupID.Valid {
		rec.FleetGroupID = row.FleetGroupID.UUID.String()
	}
	if row.CertIssuedAt.Valid {
		t := row.CertIssuedAt.Time.UTC()
		rec.CertIssuedAt = &t
	}
	if row.CertExpiresAt.Valid {
		t := row.CertExpiresAt.Time.UTC()
		rec.CertExpiresAt = &t
	}
	return rec
}

// errTxBoundStore reports that a method was invoked on a tx-bound
// Store (one returned mid-Transact) that does not own a pool handle.
// Methods that go through dbsqlc need *sql.DB, so they explicitly
// reject the tx-bound shape until the dbsqlc DBTX bridge is wired
// through Transact.
var errTxBoundStore = errors.New("postgres: method requires pool handle, not tx-bound store")

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
