package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

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

	// Q4.U-S-04: cert_serial is updated separately via
	// UpdateAgentCertSerial after a successful issuance, so PutAgent's
	// upsert doesn't need to know about it. The DEFAULT '' on the
	// schema covers the fresh-agent case.
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

// UpdateAgentCertSerial pins the latest issued client cert serial for
// pinning at gRPC connect time (Q4.U-S-04). Called from issuance flows
// (bootstrap, RenewCertificate). An empty serial clears the pin.
func (s *Store) UpdateAgentCertSerial(ctx context.Context, agentID string, serial string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agents SET cert_serial = ? WHERE id = ?`, serial, agentID)
	return err
}

// GetAgentCertSerial returns the pinned serial, or "" if none / agent
// missing. Errors propagate so the caller can fail-closed.
func (s *Store) GetAgentCertSerial(ctx context.Context, agentID string) (string, error) {
	var serial string
	err := s.db.QueryRowContext(ctx, `SELECT cert_serial FROM agents WHERE id = ?`, agentID).Scan(&serial)
	if err != nil {
		return "", err
	}
	return serial, nil
}

// UpdateAgentCertPin persists the SPKI SHA-256 hash for an agent (S-02).
func (s *Store) UpdateAgentCertPin(ctx context.Context, agentID string, pin []byte) error {
	result, err := s.db.ExecContext(ctx, `UPDATE agents SET cert_spki_sha256 = ? WHERE id = ?`, pin, agentID)
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
	err := s.db.QueryRowContext(ctx, `SELECT cert_spki_sha256 FROM agents WHERE id = ?`, agentID).Scan(&pin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return pin, nil
}

// EarliestAgentCertExpiry returns MIN(cert_expires_at_unix) or nil when
// no agent carries an expiry (P6-6.3f).
func (s *Store) EarliestAgentCertExpiry(ctx context.Context) (*time.Time, error) {
	var earliest sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MIN(cert_expires_at_unix) FROM agents`,
	).Scan(&earliest); err != nil {
		return nil, err
	}
	if !earliest.Valid {
		return nil, nil
	}
	t := fromUnix(earliest.Int64)
	return &t, nil
}

func (s *Store) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_name, fleet_group_id, version, read_only, last_seen_at_unix, cert_issued_at_unix, cert_expires_at_unix, cert_serial
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
		var certSerial sql.NullString
		if err := rows.Scan(&agent.ID, &agent.NodeName, &fleetGroupID, &agent.Version, &readOnly, &lastSeenAt, &certIssuedAtUnix, &certExpiresAtUnix, &certSerial); err != nil {
			return nil, err
		}
		if certSerial.Valid {
			agent.CertSerial = certSerial.String
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

func (s *Store) UpdateAgentFleetGroup(ctx context.Context, agentID, fleetGroupID string) error {
	var fleetGroup sql.NullString
	if fleetGroupID != "" {
		fleetGroup.Valid = true
		fleetGroup.String = fleetGroupID
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE agents
		SET fleet_group_id = ?
		WHERE id = ?
	`, fleetGroup, agentID)
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
		SET transport_mode = ?, dial_address = ?
		WHERE id = ?
	`, transportMode, addr, agentID)
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
		WHERE agent_id = ?
	`, agentID)
	return err
}
