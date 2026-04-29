package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutDiscoveredClient(ctx context.Context, record storage.DiscoveredClientRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO discovered_clients (
			id, agent_id, client_name, secret, status,
			total_octets, current_connections, active_unique_ips,
			connection_links, max_tcp_conns, max_unique_ips,
			data_quota_bytes, expiration,
			discovered_at_unix, updated_at_unix
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id, client_name) DO UPDATE SET
			status = CASE
				WHEN discovered_clients.status = 'ignored' THEN discovered_clients.status
				ELSE excluded.status
			END,
			secret = excluded.secret,
			total_octets = excluded.total_octets,
			current_connections = excluded.current_connections,
			active_unique_ips = excluded.active_unique_ips,
			connection_links = excluded.connection_links,
			max_tcp_conns = excluded.max_tcp_conns,
			max_unique_ips = excluded.max_unique_ips,
			data_quota_bytes = excluded.data_quota_bytes,
			expiration = excluded.expiration,
			updated_at_unix = excluded.updated_at_unix
	`, record.ID, record.AgentID, record.ClientName, record.Secret, record.Status,
		record.TotalOctets, record.CurrentConnections, record.ActiveUniqueIPs,
		encodeStringArray(record.ConnectionLinks), record.MaxTCPConns, record.MaxUniqueIPs,
		record.DataQuotaBytes, record.Expiration,
		toUnix(record.DiscoveredAt), toUnix(record.UpdatedAt))
	return err
}

func (s *Store) ListDiscoveredClients(ctx context.Context) ([]storage.DiscoveredClientRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, client_name, secret, status,
			total_octets, current_connections, active_unique_ips,
			connection_links, max_tcp_conns, max_unique_ips,
			data_quota_bytes, expiration,
			discovered_at_unix, updated_at_unix
		FROM discovered_clients
		ORDER BY discovered_at_unix DESC, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanDiscoveredClientRows(rows)
}

func (s *Store) ListDiscoveredClientsByAgent(ctx context.Context, agentID string) ([]storage.DiscoveredClientRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, client_name, secret, status,
			total_octets, current_connections, active_unique_ips,
			connection_links, max_tcp_conns, max_unique_ips,
			data_quota_bytes, expiration,
			discovered_at_unix, updated_at_unix
		FROM discovered_clients
		WHERE agent_id = ?
		ORDER BY discovered_at_unix DESC, id
	`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanDiscoveredClientRows(rows)
}

func (s *Store) GetDiscoveredClient(ctx context.Context, id string) (storage.DiscoveredClientRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, agent_id, client_name, secret, status,
			total_octets, current_connections, active_unique_ips,
			connection_links, max_tcp_conns, max_unique_ips,
			data_quota_bytes, expiration,
			discovered_at_unix, updated_at_unix
		FROM discovered_clients
		WHERE id = ?
	`, id)

	var record storage.DiscoveredClientRecord
	var discoveredAtUnix, updatedAtUnix int64
	var linksJSON string
	if err := row.Scan(
		&record.ID, &record.AgentID, &record.ClientName, &record.Secret, &record.Status,
		&record.TotalOctets, &record.CurrentConnections, &record.ActiveUniqueIPs,
		&linksJSON, &record.MaxTCPConns, &record.MaxUniqueIPs,
		&record.DataQuotaBytes, &record.Expiration,
		&discoveredAtUnix, &updatedAtUnix,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.DiscoveredClientRecord{}, storage.ErrNotFound
		}
		return storage.DiscoveredClientRecord{}, err
	}
	record.ConnectionLinks = decodeStringArray(linksJSON)
	record.DiscoveredAt = fromUnix(discoveredAtUnix)
	record.UpdatedAt = fromUnix(updatedAtUnix)
	return record, nil
}

func (s *Store) GetDiscoveredClientByAgentAndName(ctx context.Context, agentID string, clientName string) (storage.DiscoveredClientRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, agent_id, client_name, secret, status,
			total_octets, current_connections, active_unique_ips,
			connection_links, max_tcp_conns, max_unique_ips,
			data_quota_bytes, expiration,
			discovered_at_unix, updated_at_unix
		FROM discovered_clients
		WHERE agent_id = ? AND client_name = ?
	`, agentID, clientName)

	var record storage.DiscoveredClientRecord
	var discoveredAtUnix, updatedAtUnix int64
	var linksJSON string
	if err := row.Scan(
		&record.ID, &record.AgentID, &record.ClientName, &record.Secret, &record.Status,
		&record.TotalOctets, &record.CurrentConnections, &record.ActiveUniqueIPs,
		&linksJSON, &record.MaxTCPConns, &record.MaxUniqueIPs,
		&record.DataQuotaBytes, &record.Expiration,
		&discoveredAtUnix, &updatedAtUnix,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.DiscoveredClientRecord{}, storage.ErrNotFound
		}
		return storage.DiscoveredClientRecord{}, err
	}
	record.ConnectionLinks = decodeStringArray(linksJSON)
	record.DiscoveredAt = fromUnix(discoveredAtUnix)
	record.UpdatedAt = fromUnix(updatedAtUnix)
	return record, nil
}

func (s *Store) UpdateDiscoveredClientStatus(ctx context.Context, id string, status string, updatedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE discovered_clients SET status = ?, updated_at_unix = ? WHERE id = ?
	`, status, toUnix(updatedAt), id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// UpdateDiscoveredClientStatusBulk flips the status for every ID in
// one statement so the duplicate-secret adoption flow stays O(1) round-
// trips regardless of duplicate count (Q2.U-P-10).
func (s *Store) UpdateDiscoveredClientStatusBulk(ctx context.Context, ids []string, status string, updatedAt time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, status, toUnix(updatedAt))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := `
		UPDATE discovered_clients
		SET status = ?, updated_at_unix = ?
		WHERE id IN (` + strings.Join(placeholders, ",") + `)
	`
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) DeleteDiscoveredClient(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM discovered_clients WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func scanDiscoveredClientRows(rows *sql.Rows) ([]storage.DiscoveredClientRecord, error) {
	result := make([]storage.DiscoveredClientRecord, 0)
	for rows.Next() {
		var record storage.DiscoveredClientRecord
		var discoveredAtUnix, updatedAtUnix int64
		var linksJSON string
		if err := rows.Scan(
			&record.ID, &record.AgentID, &record.ClientName, &record.Secret, &record.Status,
			&record.TotalOctets, &record.CurrentConnections, &record.ActiveUniqueIPs,
			&linksJSON, &record.MaxTCPConns, &record.MaxUniqueIPs,
			&record.DataQuotaBytes, &record.Expiration,
			&discoveredAtUnix, &updatedAtUnix,
		); err != nil {
			return nil, err
		}
		record.ConnectionLinks = decodeStringArray(linksJSON)
		record.DiscoveredAt = fromUnix(discoveredAtUnix)
		record.UpdatedAt = fromUnix(updatedAtUnix)
		result = append(result, record)
	}
	return result, rows.Err()
}
