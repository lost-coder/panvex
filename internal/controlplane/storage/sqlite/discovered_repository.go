// internal/controlplane/storage/sqlite/discovered_repository.go
//
// discovered.Repository implementation backed by SQLite via direct database/sql
// queries. Mirrors the Postgres implementation (storage/postgres/discovered_repository.go)
// but uses ? placeholders and SQLite-specific type handling (INTEGER unix
// timestamps).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// discoveredRepository implements discovered.Repository against SQLite.
// db satisfies dbtx which is implemented by both *sql.DB and *sql.Tx,
// enabling the same code to run inside or outside a transaction.
type discoveredRepository struct {
	db dbtx
}

// NewDiscoveredRepository wires a discovered.Repository against a SQLite
// connection or transaction. Accepts *sql.DB (pool) or *sql.Tx.
// When called with a *Store, use store.DB() to pass the underlying *sql.DB.
func NewDiscoveredRepository(db dbtx) discovered.Repository {
	return &discoveredRepository{db: db}
}

// ---------------------------------------------------------------------------
// Repository methods
// ---------------------------------------------------------------------------

const discoveredClientSelectCols = `
	id, agent_id, client_name, secret, status,
	total_octets, current_connections, active_unique_ips,
	connection_links, max_tcp_conns, max_unique_ips,
	data_quota_bytes, expiration,
	discovered_at_unix, updated_at_unix`

func (r *discoveredRepository) Get(ctx context.Context, id discovered.DiscoveredID) (discovered.DiscoveredClient, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+discoveredClientSelectCols+`
		FROM discovered_clients
		WHERE id = ?
	`, string(id))
	dc, err := scanDiscoveredClient(row)
	if errors.Is(err, sql.ErrNoRows) {
		return discovered.DiscoveredClient{}, storage.ErrNotFound
	}
	if err != nil {
		return discovered.DiscoveredClient{}, fmt.Errorf("discoveredRepository.Get: %w", err)
	}
	return dc, nil
}

func (r *discoveredRepository) GetByAgentAndName(ctx context.Context, agentID, clientName string) (discovered.DiscoveredClient, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+discoveredClientSelectCols+`
		FROM discovered_clients
		WHERE agent_id = ? AND client_name = ?
	`, agentID, clientName)
	dc, err := scanDiscoveredClient(row)
	if errors.Is(err, sql.ErrNoRows) {
		return discovered.DiscoveredClient{}, storage.ErrNotFound
	}
	if err != nil {
		return discovered.DiscoveredClient{}, fmt.Errorf("discoveredRepository.GetByAgentAndName: %w", err)
	}
	return dc, nil
}

func (r *discoveredRepository) List(ctx context.Context) ([]discovered.DiscoveredClient, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+discoveredClientSelectCols+`
		FROM discovered_clients
		ORDER BY discovered_at_unix DESC, id
	`)
	if err != nil {
		return nil, fmt.Errorf("discoveredRepository.List: %w", err)
	}
	defer rows.Close()
	return scanDiscoveredClientDomainRows(rows)
}

func (r *discoveredRepository) ListByAgent(ctx context.Context, agentID string) ([]discovered.DiscoveredClient, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+discoveredClientSelectCols+`
		FROM discovered_clients
		WHERE agent_id = ?
		ORDER BY discovered_at_unix DESC, id
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("discoveredRepository.ListByAgent: %w", err)
	}
	defer rows.Close()
	return scanDiscoveredClientDomainRows(rows)
}

func (r *discoveredRepository) Save(ctx context.Context, dc discovered.DiscoveredClient) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO discovered_clients (
			id, agent_id, client_name, secret, status,
			total_octets, current_connections, active_unique_ips,
			connection_links, max_tcp_conns, max_unique_ips,
			data_quota_bytes, expiration,
			discovered_at_unix, updated_at_unix
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status              = excluded.status,
			secret              = excluded.secret,
			total_octets        = excluded.total_octets,
			current_connections = excluded.current_connections,
			active_unique_ips   = excluded.active_unique_ips,
			connection_links    = excluded.connection_links,
			max_tcp_conns       = excluded.max_tcp_conns,
			max_unique_ips      = excluded.max_unique_ips,
			data_quota_bytes    = excluded.data_quota_bytes,
			expiration          = excluded.expiration,
			discovered_at_unix  = excluded.discovered_at_unix,
			updated_at_unix     = excluded.updated_at_unix
	`,
		string(dc.ID), dc.AgentID, dc.ClientName, dc.Secret, string(dc.Status),
		int64(dc.TotalOctets),        //nolint:gosec
		int64(dc.CurrentConnections), //nolint:gosec
		int64(dc.ActiveUniqueIPs),    //nolint:gosec
		encodeStringArray(dc.ConnectionLinks),
		int64(dc.MaxTCPConns),  //nolint:gosec
		int64(dc.MaxUniqueIPs), //nolint:gosec
		dc.DataQuotaBytes, dc.Expiration,
		toUnix(dc.FirstSeen), toUnix(dc.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("discoveredRepository.Save: %w", err)
	}
	return nil
}

func (r *discoveredRepository) UpdateStatus(ctx context.Context, id discovered.DiscoveredID, status discovered.Status, observedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE discovered_clients SET status = ?, updated_at_unix = ? WHERE id = ?
	`, string(status), toUnix(observedAt), string(id))
	if err != nil {
		return fmt.Errorf("discoveredRepository.UpdateStatus: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("discoveredRepository.UpdateStatus rows: %w", err)
	}
	if affected == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// UpdateStatusBulk updates status for all ids in one statement using an IN
// clause so the adoption flow stays O(1) round-trips regardless of count.
func (r *discoveredRepository) UpdateStatusBulk(ctx context.Context, ids []discovered.DiscoveredID, status discovered.Status, observedAt time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, string(status), toUnix(observedAt))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, string(id))
	}
	query := `UPDATE discovered_clients SET status = ?, updated_at_unix = ? WHERE id IN (` +
		strings.Join(placeholders, ",") + `)`
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("discoveredRepository.UpdateStatusBulk: %w", err)
	}
	return nil
}

func (r *discoveredRepository) Delete(ctx context.Context, id discovered.DiscoveredID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM discovered_clients WHERE id = ?`, string(id))
	if err != nil {
		return fmt.Errorf("discoveredRepository.Delete: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("discoveredRepository.Delete rows: %w", err)
	}
	if affected == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// Scan helpers
// ---------------------------------------------------------------------------

type discoveredClientScanner interface {
	Scan(dest ...any) error
}

func scanDiscoveredClient(s discoveredClientScanner) (discovered.DiscoveredClient, error) {
	var (
		dc             discovered.DiscoveredClient
		id             string
		agentID        string
		status         string
		connectionJSON string
		firstSeenUnix  int64
		updatedAtUnix  int64
	)
	if err := s.Scan(
		&id, &agentID, &dc.ClientName, &dc.Secret, &status,
		&dc.TotalOctets, &dc.CurrentConnections, &dc.ActiveUniqueIPs,
		&connectionJSON, &dc.MaxTCPConns, &dc.MaxUniqueIPs,
		&dc.DataQuotaBytes, &dc.Expiration,
		&firstSeenUnix, &updatedAtUnix,
	); err != nil {
		return discovered.DiscoveredClient{}, err
	}
	dc.ID = discovered.DiscoveredID(id)
	dc.AgentID = agentID
	dc.Status = discovered.Status(status)
	dc.ConnectionLinks = decodeStringArray(connectionJSON)
	dc.FirstSeen = fromUnix(firstSeenUnix)
	dc.UpdatedAt = fromUnix(updatedAtUnix)
	return dc, nil
}

func scanDiscoveredClientDomainRows(rows *sql.Rows) ([]discovered.DiscoveredClient, error) {
	var result []discovered.DiscoveredClient
	for rows.Next() {
		dc, err := scanDiscoveredClient(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, dc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		return []discovered.DiscoveredClient{}, nil
	}
	return result, nil
}
