package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// encodeStringArray serializes a []string for storage in a JSONB
// column. Nil/empty arrays become `[]` so the column never holds NULL
// or a malformed value.
func encodeStringArray(values []string) []byte {
	if len(values) == 0 {
		return []byte("[]")
	}
	b, err := json.Marshal(values)
	if err != nil {
		return []byte("[]")
	}
	return b
}

// decodeStringArray inverts encodeStringArray. Empty/invalid input
// returns nil.
func decodeStringArray(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func (s *Store) PutClient(ctx context.Context, client storage.ClientRecord) error {
	var deletedAt sql.NullTime
	if client.DeletedAt != nil {
		deletedAt.Valid = true
		deletedAt.Time = client.DeletedAt.UTC()
	}

	// L-23: RETURNING id lets us verify the upsert actually landed.
	// Without it a subtle ON CONFLICT no-op (e.g. a constraint
	// triggering DO NOTHING somewhere upstream of this code) would
	// silently leave the row stale; the explicit Scan + ID check
	// turns that into a loud error instead.
	var returnedID string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO clients (
			id,
			name,
			secret_ciphertext,
			user_ad_tag,
			enabled,
			max_tcp_conns,
			max_unique_ips,
			data_quota_bytes,
			expiration_rfc3339,
			created_at,
			updated_at,
			deleted_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO UPDATE
		SET name = EXCLUDED.name,
		    secret_ciphertext = EXCLUDED.secret_ciphertext,
		    user_ad_tag = EXCLUDED.user_ad_tag,
		    enabled = EXCLUDED.enabled,
		    max_tcp_conns = EXCLUDED.max_tcp_conns,
		    max_unique_ips = EXCLUDED.max_unique_ips,
		    data_quota_bytes = EXCLUDED.data_quota_bytes,
		    expiration_rfc3339 = EXCLUDED.expiration_rfc3339,
		    created_at = EXCLUDED.created_at,
		    updated_at = EXCLUDED.updated_at,
		    deleted_at = EXCLUDED.deleted_at
		RETURNING id
	`, client.ID, client.Name, client.SecretCiphertext, client.UserADTag, client.Enabled, client.MaxTCPConns, client.MaxUniqueIPs, client.DataQuotaBytes, client.ExpirationRFC3339, client.CreatedAt.UTC(), client.UpdatedAt.UTC(), deletedAt).Scan(&returnedID)
	if err != nil {
		return err
	}
	if returnedID != client.ID {
		return fmt.Errorf("postgres: PutClient upsert returned id %q, want %q", returnedID, client.ID)
	}
	return nil
}

func (s *Store) GetClientByID(ctx context.Context, clientID string) (storage.ClientRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			name,
			secret_ciphertext,
			user_ad_tag,
			enabled,
			max_tcp_conns,
			max_unique_ips,
			data_quota_bytes,
			expiration_rfc3339,
			created_at,
			updated_at,
			deleted_at
		FROM clients
		WHERE id = $1
	`, clientID)

	return scanClientRow(row)
}

func (s *Store) ListClients(ctx context.Context) ([]storage.ClientRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			name,
			secret_ciphertext,
			user_ad_tag,
			enabled,
			max_tcp_conns,
			max_unique_ips,
			data_quota_bytes,
			expiration_rfc3339,
			created_at,
			updated_at,
			deleted_at
		FROM clients
		ORDER BY created_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.ClientRecord, 0)
	for rows.Next() {
		client, err := scanClientRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, client)
	}

	return result, rows.Err()
}

func scanClientRow(scanner interface {
	Scan(dest ...any) error
}) (storage.ClientRecord, error) {
	var client storage.ClientRecord
	var deletedAt sql.NullTime
	if err := scanner.Scan(
		&client.ID,
		&client.Name,
		&client.SecretCiphertext,
		&client.UserADTag,
		&client.Enabled,
		&client.MaxTCPConns,
		&client.MaxUniqueIPs,
		&client.DataQuotaBytes,
		&client.ExpirationRFC3339,
		&client.CreatedAt,
		&client.UpdatedAt,
		&deletedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.ClientRecord{}, storage.ErrNotFound
		}
		return storage.ClientRecord{}, err
	}

	client.CreatedAt = client.CreatedAt.UTC()
	client.UpdatedAt = client.UpdatedAt.UTC()
	if deletedAt.Valid {
		timeValue := deletedAt.Time.UTC()
		client.DeletedAt = &timeValue
	}

	return client, nil
}

func (s *Store) PutClientAssignment(ctx context.Context, assignment storage.ClientAssignmentRecord) error {
	var fleetGroupID sql.NullString
	if assignment.FleetGroupID != "" {
		fleetGroupID.Valid = true
		fleetGroupID.String = assignment.FleetGroupID
	}
	var agentID sql.NullString
	if assignment.AgentID != "" {
		agentID.Valid = true
		agentID.String = assignment.AgentID
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO client_assignments (id, client_id, target_type, fleet_group_id, agent_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE
		SET client_id = EXCLUDED.client_id,
		    target_type = EXCLUDED.target_type,
		    fleet_group_id = EXCLUDED.fleet_group_id,
		    agent_id = EXCLUDED.agent_id,
		    created_at = EXCLUDED.created_at
	`, assignment.ID, assignment.ClientID, assignment.TargetType, fleetGroupID, agentID, assignment.CreatedAt.UTC())
	return err
}

func (s *Store) DeleteClientAssignments(ctx context.Context, clientID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM client_assignments
		WHERE client_id = $1
	`, clientID)
	return err
}

func (s *Store) ListClientAssignments(ctx context.Context, clientID string) ([]storage.ClientAssignmentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, client_id, target_type, fleet_group_id, agent_id, created_at
		FROM client_assignments
		WHERE client_id = $1
		ORDER BY created_at, id
	`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.ClientAssignmentRecord, 0)
	for rows.Next() {
		var assignment storage.ClientAssignmentRecord
		var fleetGroupID sql.NullString
		var agentID sql.NullString
		if err := rows.Scan(&assignment.ID, &assignment.ClientID, &assignment.TargetType, &fleetGroupID, &agentID, &assignment.CreatedAt); err != nil {
			return nil, err
		}
		if fleetGroupID.Valid {
			assignment.FleetGroupID = fleetGroupID.String
		}
		if agentID.Valid {
			assignment.AgentID = agentID.String
		}
		assignment.CreatedAt = assignment.CreatedAt.UTC()
		result = append(result, assignment)
	}

	return result, rows.Err()
}

func (s *Store) PutClientDeployment(ctx context.Context, deployment storage.ClientDeploymentRecord) error {
	var lastAppliedAt sql.NullTime
	if deployment.LastAppliedAt != nil {
		lastAppliedAt.Valid = true
		lastAppliedAt.Time = deployment.LastAppliedAt.UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO client_deployments (
			client_id,
			agent_id,
			desired_operation,
			status,
			last_error,
			connection_links,
			link_diagnostic,
			last_applied_at,
			updated_at,
			last_reset_epoch_secs
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (client_id, agent_id) DO UPDATE
		SET desired_operation = EXCLUDED.desired_operation,
		    status = EXCLUDED.status,
		    last_error = EXCLUDED.last_error,
		    connection_links = EXCLUDED.connection_links,
		    link_diagnostic = EXCLUDED.link_diagnostic,
		    last_applied_at = EXCLUDED.last_applied_at,
		    updated_at = EXCLUDED.updated_at,
		    last_reset_epoch_secs = EXCLUDED.last_reset_epoch_secs
	`, deployment.ClientID, deployment.AgentID, deployment.DesiredOperation, deployment.Status, deployment.LastError, encodeStringArray(deployment.ConnectionLinks), deployment.LinkDiagnostic, lastAppliedAt, deployment.UpdatedAt.UTC(), int64(deployment.LastResetEpochSecs)) //nolint:gosec
	return err
}

func (s *Store) ListClientDeployments(ctx context.Context, clientID string) ([]storage.ClientDeploymentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT client_id, agent_id, desired_operation, status, last_error, connection_links, link_diagnostic, last_applied_at, updated_at, last_reset_epoch_secs
		FROM client_deployments
		WHERE client_id = $1
		ORDER BY agent_id
	`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.ClientDeploymentRecord, 0)
	for rows.Next() {
		var deployment storage.ClientDeploymentRecord
		var lastAppliedAt sql.NullTime
		var linksJSON []byte
		var lastReset int64
		if err := rows.Scan(&deployment.ClientID, &deployment.AgentID, &deployment.DesiredOperation, &deployment.Status, &deployment.LastError, &linksJSON, &deployment.LinkDiagnostic, &lastAppliedAt, &deployment.UpdatedAt, &lastReset); err != nil {
			return nil, err
		}
		deployment.ConnectionLinks = decodeStringArray(linksJSON)
		if lastAppliedAt.Valid {
			timeValue := lastAppliedAt.Time.UTC()
			deployment.LastAppliedAt = &timeValue
		}
		deployment.UpdatedAt = deployment.UpdatedAt.UTC()
		deployment.LastResetEpochSecs = uint64(lastReset) //nolint:gosec
		result = append(result, deployment)
	}

	return result, rows.Err()
}

func (s *Store) UpsertClientUsage(ctx context.Context, r storage.ClientUsageRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO client_usage (
			client_id, agent_id, traffic_used_bytes, unique_ips_used,
			active_tcp_conns, active_unique_ips, last_seq, observed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (client_id, agent_id) DO UPDATE SET
			traffic_used_bytes = EXCLUDED.traffic_used_bytes,
			unique_ips_used    = EXCLUDED.unique_ips_used,
			active_tcp_conns   = EXCLUDED.active_tcp_conns,
			active_unique_ips  = EXCLUDED.active_unique_ips,
			last_seq           = EXCLUDED.last_seq,
			observed_at        = EXCLUDED.observed_at
	`,
		r.ClientID, r.AgentID,
		int64(r.TrafficUsedBytes), r.UniqueIPsUsed,
		r.ActiveTCPConns, r.ActiveUniqueIPs,
		int64(r.LastSeq), r.ObservedAt.UTC())
	return err
}

func (s *Store) ListClientUsage(ctx context.Context) ([]storage.ClientUsageRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT client_id, agent_id, traffic_used_bytes, unique_ips_used,
			active_tcp_conns, active_unique_ips, last_seq, observed_at
		FROM client_usage
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.ClientUsageRecord, 0)
	for rows.Next() {
		var r storage.ClientUsageRecord
		var traffic, lastSeq int64
		if err := rows.Scan(&r.ClientID, &r.AgentID, &traffic, &r.UniqueIPsUsed,
			&r.ActiveTCPConns, &r.ActiveUniqueIPs, &lastSeq, &r.ObservedAt); err != nil {
			return nil, err
		}
		r.TrafficUsedBytes = uint64(traffic)
		r.LastSeq = uint64(lastSeq)
		r.ObservedAt = r.ObservedAt.UTC()
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) DeleteClientUsageByClient(ctx context.Context, clientID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM client_usage WHERE client_id = $1`, clientID)
	return err
}
