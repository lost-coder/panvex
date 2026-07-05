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

// PutAgent upserts one agent row.
//
// Phase-3 §3.1 (continued): now goes through dbsqlc.UpsertAgent.
// agentRecordToUpsertParams below is the domain-DTO → SQL-row bridge —
// future PutAgent callers gain compile-time type safety on every
// column from the sqlc-generated UpsertAgentParams.
//
// Uses s.db (the dbExecutor) rather than the pool-only s.sqlDB so the
// upsert composes inside Transact — the inbound enrollment flow calls
// this as tx.PutAgent(...) from within a transaction. dbsqlc.New accepts
// any DBTX, which dbExecutor satisfies (both *sql.DB and *sql.Tx fit).
func (s *Store) PutAgent(ctx context.Context, agent storage.AgentRecord) error {
	return dbsqlc.New(s.db).UpsertAgent(ctx, agentRecordToUpsertParams(agent))
}

// agentRecordToUpsertParams is the domain-DTO → dbsqlc params bridge.
// Kept private to the postgres package: callers see only storage.AgentRecord.
func agentRecordToUpsertParams(agent storage.AgentRecord) dbsqlc.UpsertAgentParams {
	params := dbsqlc.UpsertAgentParams{
		ID:         agent.ID,
		NodeName:   agent.NodeName,
		Version:    agent.Version,
		ReadOnly:   agent.ReadOnly,
		LastSeenAt: agent.LastSeenAt.UTC(),
	}
	if agent.FleetGroupID != "" {
		if id, err := uuid.Parse(agent.FleetGroupID); err == nil {
			params.FleetGroupID = uuid.NullUUID{UUID: id, Valid: true}
		}
	}
	if agent.CertIssuedAt != nil {
		params.CertIssuedAt = sql.NullTime{Time: agent.CertIssuedAt.UTC(), Valid: true}
	}
	if agent.CertExpiresAt != nil {
		params.CertExpiresAt = sql.NullTime{Time: agent.CertExpiresAt.UTC(), Valid: true}
	}
	return params
}

// ListAgents returns every agent the panel knows about, ordered by
// last_seen_at + id for stable pagination.
//
// Phase-3 §3.1: this is the first method to consume the sqlc-generated
// dbsqlc.Queries surface. Conversion from dbsqlc.ListAgentsRow to the
// storage.AgentRecord shape lives in agentRecordFromRow below; if a
// future query gets migrated, that helper stays the only place that
// knows about the SQL → domain mapping.
// EarliestAgentCertExpiry returns MIN(cert_expires_at) or nil when no
// agent carries an expiry (P6-6.3f).
func (s *Store) EarliestAgentCertExpiry(ctx context.Context) (*time.Time, error) {
	var earliest sql.NullTime
	if err := s.db.QueryRowContext(ctx,
		`SELECT MIN(cert_expires_at) FROM agents`,
	).Scan(&earliest); err != nil {
		return nil, err
	}
	if !earliest.Valid {
		return nil, nil
	}
	t := earliest.Time.UTC()
	return &t, nil
}

func (s *Store) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	rows, err := dbsqlc.New(s.db).ListAgents(ctx)
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

// UpdateAgentCertSerial pins the latest issued client cert serial
// (Q4.U-S-04). Called after each successful issuance.
func (s *Store) UpdateAgentCertSerial(ctx context.Context, agentID string, serial string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agents SET cert_serial = $2 WHERE id = $1`, agentID, serial)
	return err
}

// GetAgentCertSerial returns the pinned serial for the given agent.
func (s *Store) GetAgentCertSerial(ctx context.Context, agentID string) (string, error) {
	var serial sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT cert_serial FROM agents WHERE id = $1`, agentID).Scan(&serial)
	if err != nil {
		return "", err
	}
	if !serial.Valid {
		return "", nil
	}
	return serial.String, nil
}

// UpdateAgentCertPin persists the SPKI SHA-256 hash for an agent (S-02).
func (s *Store) UpdateAgentCertPin(ctx context.Context, agentID string, pin []byte) error {
	result, err := s.db.ExecContext(ctx, `UPDATE agents SET cert_spki_sha256 = $2 WHERE id = $1`, agentID, pin)
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

// GetAgentCertPin returns the SPKI pin for the agent. Returns ErrNotFound
// when no agent with the given ID exists; returns empty bytes (no error)
// when the agent exists but is not yet pinned.
func (s *Store) GetAgentCertPin(ctx context.Context, agentID string) ([]byte, error) {
	var pin []byte
	err := s.db.QueryRowContext(ctx, `SELECT cert_spki_sha256 FROM agents WHERE id = $1`, agentID).Scan(&pin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return pin, nil
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

func (s *Store) UpdateAgentFleetGroup(ctx context.Context, agentID, fleetGroupID string) error {
	var fleetGroup sql.NullString
	if fleetGroupID != "" {
		fleetGroup.Valid = true
		fleetGroup.String = fleetGroupID
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE agents
		SET fleet_group_id = $2
		WHERE id = $1
	`, agentID, fleetGroup)
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

func (s *Store) UpdateAgentTransportMode(ctx context.Context, agentID, transportMode, dialAddress string) error {
	var addr sql.NullString
	if dialAddress != "" {
		addr = sql.NullString{String: dialAddress, Valid: true}
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE agents
		SET transport_mode = $2, dial_address = $3
		WHERE id = $1
	`, agentID, transportMode, addr)
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
		DELETE FROM telemt_instances
		WHERE agent_id = $1
	`, agentID)
	return err
}
