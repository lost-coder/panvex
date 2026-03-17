package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

func (s *Store) PutClient(ctx context.Context, client storage.ClientRecord) error {
	var deletedAt sql.NullInt64
	if client.DeletedAt != nil {
		deletedAt.Valid = true
		deletedAt.Int64 = toUnix(*client.DeletedAt)
	}

	_, err := s.db.ExecContext(ctx, `
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
			created_at_unix,
			updated_at_unix,
			deleted_at_unix
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			secret_ciphertext = excluded.secret_ciphertext,
			user_ad_tag = excluded.user_ad_tag,
			enabled = excluded.enabled,
			max_tcp_conns = excluded.max_tcp_conns,
			max_unique_ips = excluded.max_unique_ips,
			data_quota_bytes = excluded.data_quota_bytes,
			expiration_rfc3339 = excluded.expiration_rfc3339,
			created_at_unix = excluded.created_at_unix,
			updated_at_unix = excluded.updated_at_unix,
			deleted_at_unix = excluded.deleted_at_unix
	`, client.ID, client.Name, client.SecretCiphertext, client.UserADTag, boolToInt(client.Enabled), client.MaxTCPConns, client.MaxUniqueIPs, client.DataQuotaBytes, client.ExpirationRFC3339, toUnix(client.CreatedAt), toUnix(client.UpdatedAt), deletedAt)
	return err
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
			created_at_unix,
			updated_at_unix,
			deleted_at_unix
		FROM clients
		WHERE id = ?
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
			created_at_unix,
			updated_at_unix,
			deleted_at_unix
		FROM clients
		ORDER BY created_at_unix, id
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
	var enabled int
	var createdAt int64
	var updatedAt int64
	var deletedAt sql.NullInt64
	if err := scanner.Scan(
		&client.ID,
		&client.Name,
		&client.SecretCiphertext,
		&client.UserADTag,
		&enabled,
		&client.MaxTCPConns,
		&client.MaxUniqueIPs,
		&client.DataQuotaBytes,
		&client.ExpirationRFC3339,
		&createdAt,
		&updatedAt,
		&deletedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.ClientRecord{}, storage.ErrNotFound
		}
		return storage.ClientRecord{}, err
	}

	client.Enabled = intToBool(enabled)
	client.CreatedAt = fromUnix(createdAt)
	client.UpdatedAt = fromUnix(updatedAt)
	if deletedAt.Valid {
		timeValue := fromUnix(deletedAt.Int64)
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
		INSERT INTO client_assignments (id, client_id, target_type, fleet_group_id, agent_id, created_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			client_id = excluded.client_id,
			target_type = excluded.target_type,
			fleet_group_id = excluded.fleet_group_id,
			agent_id = excluded.agent_id,
			created_at_unix = excluded.created_at_unix
	`, assignment.ID, assignment.ClientID, assignment.TargetType, fleetGroupID, agentID, toUnix(assignment.CreatedAt))
	return err
}

func (s *Store) DeleteClientAssignments(ctx context.Context, clientID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM client_assignments
		WHERE client_id = ?
	`, clientID)
	return err
}

func (s *Store) ListClientAssignments(ctx context.Context, clientID string) ([]storage.ClientAssignmentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, client_id, target_type, fleet_group_id, agent_id, created_at_unix
		FROM client_assignments
		WHERE client_id = ?
		ORDER BY created_at_unix, id
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
		var createdAt int64
		if err := rows.Scan(&assignment.ID, &assignment.ClientID, &assignment.TargetType, &fleetGroupID, &agentID, &createdAt); err != nil {
			return nil, err
		}
		if fleetGroupID.Valid {
			assignment.FleetGroupID = fleetGroupID.String
		}
		if agentID.Valid {
			assignment.AgentID = agentID.String
		}
		assignment.CreatedAt = fromUnix(createdAt)
		result = append(result, assignment)
	}

	return result, rows.Err()
}

func (s *Store) PutClientDeployment(ctx context.Context, deployment storage.ClientDeploymentRecord) error {
	var lastAppliedAt sql.NullInt64
	if deployment.LastAppliedAt != nil {
		lastAppliedAt.Valid = true
		lastAppliedAt.Int64 = toUnix(*deployment.LastAppliedAt)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO client_deployments (
			client_id,
			agent_id,
			desired_operation,
			status,
			last_error,
			connection_link,
			last_applied_at_unix,
			updated_at_unix
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(client_id, agent_id) DO UPDATE SET
			desired_operation = excluded.desired_operation,
			status = excluded.status,
			last_error = excluded.last_error,
			connection_link = excluded.connection_link,
			last_applied_at_unix = excluded.last_applied_at_unix,
			updated_at_unix = excluded.updated_at_unix
	`, deployment.ClientID, deployment.AgentID, deployment.DesiredOperation, deployment.Status, deployment.LastError, deployment.ConnectionLink, lastAppliedAt, toUnix(deployment.UpdatedAt))
	return err
}

func (s *Store) ListClientDeployments(ctx context.Context, clientID string) ([]storage.ClientDeploymentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT client_id, agent_id, desired_operation, status, last_error, connection_link, last_applied_at_unix, updated_at_unix
		FROM client_deployments
		WHERE client_id = ?
		ORDER BY agent_id
	`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.ClientDeploymentRecord, 0)
	for rows.Next() {
		var deployment storage.ClientDeploymentRecord
		var lastAppliedAt sql.NullInt64
		var updatedAt int64
		if err := rows.Scan(&deployment.ClientID, &deployment.AgentID, &deployment.DesiredOperation, &deployment.Status, &deployment.LastError, &deployment.ConnectionLink, &lastAppliedAt, &updatedAt); err != nil {
			return nil, err
		}
		if lastAppliedAt.Valid {
			timeValue := fromUnix(lastAppliedAt.Int64)
			deployment.LastAppliedAt = &timeValue
		}
		deployment.UpdatedAt = fromUnix(updatedAt)
		result = append(result, deployment)
	}

	return result, rows.Err()
}
