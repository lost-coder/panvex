package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

func (s *Store) PutDiscoveredClient(ctx context.Context, record storage.DiscoveredClientRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO discovered_clients (
			id, agent_id, client_name, secret, status,
			total_octets, current_connections, active_unique_ips,
			connection_link, max_tcp_conns, max_unique_ips,
			data_quota_bytes, expiration,
			discovered_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (agent_id, client_name) DO UPDATE
		SET status = CASE
				WHEN discovered_clients.status = 'ignored' THEN discovered_clients.status
				ELSE EXCLUDED.status
			END,
		    secret = EXCLUDED.secret,
		    total_octets = EXCLUDED.total_octets,
		    current_connections = EXCLUDED.current_connections,
		    active_unique_ips = EXCLUDED.active_unique_ips,
		    connection_link = EXCLUDED.connection_link,
		    max_tcp_conns = EXCLUDED.max_tcp_conns,
		    max_unique_ips = EXCLUDED.max_unique_ips,
		    data_quota_bytes = EXCLUDED.data_quota_bytes,
		    expiration = EXCLUDED.expiration,
		    updated_at = EXCLUDED.updated_at
	`, record.ID, record.AgentID, record.ClientName, record.Secret, record.Status,
		record.TotalOctets, record.CurrentConnections, record.ActiveUniqueIPs,
		record.ConnectionLink, record.MaxTCPConns, record.MaxUniqueIPs,
		record.DataQuotaBytes, record.Expiration,
		record.DiscoveredAt.UTC(), record.UpdatedAt.UTC())
	return err
}

func (s *Store) ListDiscoveredClients(ctx context.Context) ([]storage.DiscoveredClientRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, client_name, secret, status,
			total_octets, current_connections, active_unique_ips,
			connection_link, max_tcp_conns, max_unique_ips,
			data_quota_bytes, expiration,
			discovered_at, updated_at
		FROM discovered_clients
		ORDER BY discovered_at DESC, id
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
			connection_link, max_tcp_conns, max_unique_ips,
			data_quota_bytes, expiration,
			discovered_at, updated_at
		FROM discovered_clients
		WHERE agent_id = $1
		ORDER BY discovered_at DESC, id
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
			connection_link, max_tcp_conns, max_unique_ips,
			data_quota_bytes, expiration,
			discovered_at, updated_at
		FROM discovered_clients
		WHERE id = $1
	`, id)

	var record storage.DiscoveredClientRecord
	if err := row.Scan(
		&record.ID, &record.AgentID, &record.ClientName, &record.Secret, &record.Status,
		&record.TotalOctets, &record.CurrentConnections, &record.ActiveUniqueIPs,
		&record.ConnectionLink, &record.MaxTCPConns, &record.MaxUniqueIPs,
		&record.DataQuotaBytes, &record.Expiration,
		&record.DiscoveredAt, &record.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.DiscoveredClientRecord{}, storage.ErrNotFound
		}
		return storage.DiscoveredClientRecord{}, err
	}
	record.DiscoveredAt = record.DiscoveredAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record, nil
}

func (s *Store) UpdateDiscoveredClientStatus(ctx context.Context, id string, status string, updatedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE discovered_clients SET status = $1, updated_at = $2 WHERE id = $3
	`, status, updatedAt.UTC(), id)
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

func (s *Store) DeleteDiscoveredClient(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM discovered_clients WHERE id = $1`, id)
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
		if err := rows.Scan(
			&record.ID, &record.AgentID, &record.ClientName, &record.Secret, &record.Status,
			&record.TotalOctets, &record.CurrentConnections, &record.ActiveUniqueIPs,
			&record.ConnectionLink, &record.MaxTCPConns, &record.MaxUniqueIPs,
			&record.DataQuotaBytes, &record.Expiration,
			&record.DiscoveredAt, &record.UpdatedAt,
		); err != nil {
			return nil, err
		}
		record.DiscoveredAt = record.DiscoveredAt.UTC()
		record.UpdatedAt = record.UpdatedAt.UTC()
		result = append(result, record)
	}
	return result, rows.Err()
}
