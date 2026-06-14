package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// encodeStringArray serializes a []string for storage in a JSON-typed
// column. Nil/empty arrays become `[]` so the column never holds NULL
// or a malformed value.
func encodeStringArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// decodeStringArray inverts encodeStringArray. Empty/invalid JSON
// returns nil so callers can distinguish "no links" from a parse
// failure only via the error path; here we treat both as "no links"
// because the column is non-null-defaulted to `[]`.
func decodeStringArray(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func (s *Store) PutClient(ctx context.Context, client storage.ClientRecord) error {
	var deletedAt sql.NullInt64
	if client.DeletedAt != nil {
		deletedAt.Valid = true
		deletedAt.Int64 = toUnix(*client.DeletedAt)
	}

	// L-23: RETURNING id mirrors the postgres path so a silent
	// no-op upsert (constraint trigger downgrading to DO NOTHING,
	// schema drift, etc.) becomes an explicit error instead of a
	// stale read on the next GetClientByID.
	subscriptionToken := sql.NullString{String: client.SubscriptionToken, Valid: client.SubscriptionToken != ""}

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
			subscription_token,
			created_at_unix,
			updated_at_unix,
			deleted_at_unix
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			secret_ciphertext = excluded.secret_ciphertext,
			user_ad_tag = excluded.user_ad_tag,
			enabled = excluded.enabled,
			max_tcp_conns = excluded.max_tcp_conns,
			max_unique_ips = excluded.max_unique_ips,
			data_quota_bytes = excluded.data_quota_bytes,
			expiration_rfc3339 = excluded.expiration_rfc3339,
			subscription_token = excluded.subscription_token,
			created_at_unix = excluded.created_at_unix,
			updated_at_unix = excluded.updated_at_unix,
			deleted_at_unix = excluded.deleted_at_unix
		RETURNING id
	`, client.ID, client.Name, client.SecretCiphertext, client.UserADTag, boolToInt(client.Enabled), client.MaxTCPConns, client.MaxUniqueIPs, client.DataQuotaBytes, client.ExpirationRFC3339, subscriptionToken, toUnix(client.CreatedAt), toUnix(client.UpdatedAt), deletedAt).Scan(&returnedID)
	if err != nil {
		return err
	}
	if returnedID != client.ID {
		return fmt.Errorf("sqlite: PutClient upsert returned id %q, want %q", returnedID, client.ID)
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
			subscription_token,
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
			subscription_token,
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
	var subscriptionToken sql.NullString
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
		&subscriptionToken,
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
	client.SubscriptionToken = subscriptionToken.String // NULL → ""
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
			connection_links,
			link_diagnostic,
			last_applied_at_unix,
			updated_at_unix,
			last_reset_epoch_secs
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(client_id, agent_id) DO UPDATE SET
			desired_operation = excluded.desired_operation,
			status = excluded.status,
			last_error = excluded.last_error,
			connection_links = excluded.connection_links,
			link_diagnostic = excluded.link_diagnostic,
			last_applied_at_unix = excluded.last_applied_at_unix,
			updated_at_unix = excluded.updated_at_unix,
			last_reset_epoch_secs = excluded.last_reset_epoch_secs
	`, deployment.ClientID, deployment.AgentID, deployment.DesiredOperation, deployment.Status, deployment.LastError, encodeStringArray(deployment.ConnectionLinks), deployment.LinkDiagnostic, lastAppliedAt, toUnix(deployment.UpdatedAt), int64(deployment.LastResetEpochSecs)) //nolint:gosec
	return err
}

func (s *Store) ListClientDeployments(ctx context.Context, clientID string) ([]storage.ClientDeploymentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT client_id, agent_id, desired_operation, status, last_error, connection_links, link_diagnostic, last_applied_at_unix, updated_at_unix, last_reset_epoch_secs
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
		var linksJSON string
		var lastReset int64
		if err := rows.Scan(&deployment.ClientID, &deployment.AgentID, &deployment.DesiredOperation, &deployment.Status, &deployment.LastError, &linksJSON, &deployment.LinkDiagnostic, &lastAppliedAt, &updatedAt, &lastReset); err != nil {
			return nil, err
		}
		deployment.ConnectionLinks = decodeStringArray(linksJSON)
		if lastAppliedAt.Valid {
			timeValue := fromUnix(lastAppliedAt.Int64)
			deployment.LastAppliedAt = &timeValue
		}
		deployment.UpdatedAt = fromUnix(updatedAt)
		deployment.LastResetEpochSecs = uint64(lastReset) //nolint:gosec
		result = append(result, deployment)
	}

	return result, rows.Err()
}

func (s *Store) UpsertClientUsage(ctx context.Context, r storage.ClientUsageRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO client_usage (
			client_id, agent_id, traffic_used_bytes, unique_ips_used,
			active_tcp_conns, active_unique_ips, last_seq, observed_at_unix
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(client_id, agent_id) DO UPDATE SET
			traffic_used_bytes = excluded.traffic_used_bytes,
			unique_ips_used    = excluded.unique_ips_used,
			active_tcp_conns   = excluded.active_tcp_conns,
			active_unique_ips  = excluded.active_unique_ips,
			last_seq           = excluded.last_seq,
			observed_at_unix   = excluded.observed_at_unix
	`,
		r.ClientID, r.AgentID,
		int64(r.TrafficUsedBytes), r.UniqueIPsUsed,
		r.ActiveTCPConns, r.ActiveUniqueIPs,
		int64(r.LastSeq), toUnix(r.ObservedAt))
	return err
}

func (s *Store) ListClientUsage(ctx context.Context) ([]storage.ClientUsageRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT client_id, agent_id, traffic_used_bytes, unique_ips_used,
			active_tcp_conns, active_unique_ips, last_seq, observed_at_unix
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
		var observedAt int64
		if err := rows.Scan(&r.ClientID, &r.AgentID, &traffic, &r.UniqueIPsUsed,
			&r.ActiveTCPConns, &r.ActiveUniqueIPs, &lastSeq, &observedAt); err != nil {
			return nil, err
		}
		r.TrafficUsedBytes = uint64(traffic)
		r.LastSeq = uint64(lastSeq)
		r.ObservedAt = fromUnix(observedAt)
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) DeleteClientUsageByClient(ctx context.Context, clientID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM client_usage WHERE client_id = ?`, clientID)
	return err
}
