// internal/controlplane/storage/postgres/discovered_repository.go
//
// discovered.Repository implementation backed by Postgres via dbsqlc.
// This is the authoritative persistence layer for the discovered domain;
// it replaces ad-hoc raw-SQL methods on Store for new callers.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// discoveredRepository implements discovered.Repository against Postgres via
// the dbsqlc query layer.
type discoveredRepository struct {
	q  *dbsqlc.Queries
	db dbsqlc.DBTX // kept for queries not yet in dbsqlc (ListByAgent)
}

// NewDiscoveredRepository wires a discovered.Repository against a Postgres
// connection or transaction. db may be *sql.DB (pool) or *sql.Tx.
func NewDiscoveredRepository(db dbsqlc.DBTX) discovered.Repository {
	return &discoveredRepository{q: dbsqlc.New(db), db: db}
}

// ---------------------------------------------------------------------------
// Repository methods
// ---------------------------------------------------------------------------

func (r *discoveredRepository) Get(ctx context.Context, id discovered.DiscoveredID) (discovered.DiscoveredClient, error) {
	row, err := r.q.GetDiscoveredClient(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return discovered.DiscoveredClient{}, storage.ErrNotFound
	}
	if err != nil {
		return discovered.DiscoveredClient{}, fmt.Errorf("discoveredRepository.Get: %w", err)
	}
	return pgGetRowToDiscoveredClient(row), nil
}

func (r *discoveredRepository) GetByAgentAndName(ctx context.Context, agentID, clientName string) (discovered.DiscoveredClient, error) {
	row, err := r.q.GetDiscoveredClientByAgentAndName(ctx, dbsqlc.GetDiscoveredClientByAgentAndNameParams{
		AgentID:    agentID,
		ClientName: clientName,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return discovered.DiscoveredClient{}, storage.ErrNotFound
	}
	if err != nil {
		return discovered.DiscoveredClient{}, fmt.Errorf("discoveredRepository.GetByAgentAndName: %w", err)
	}
	return pgGetByAgentAndNameRowToDiscoveredClient(row), nil
}

func (r *discoveredRepository) List(ctx context.Context) ([]discovered.DiscoveredClient, error) {
	rows, err := r.q.ListDiscoveredClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("discoveredRepository.List: %w", err)
	}
	out := make([]discovered.DiscoveredClient, len(rows))
	for i, row := range rows {
		out[i] = pgListRowToDiscoveredClient(row)
	}
	return out, nil
}

// ListByAgent returns all discovered clients for the given agent. dbsqlc does
// not yet have a generated method for this query, so we use a raw query via
// the stored DBTX.
func (r *discoveredRepository) ListByAgent(ctx context.Context, agentID string) ([]discovered.DiscoveredClient, error) {
	const q = `
SELECT id, agent_id, client_name, secret, status,
       total_octets, current_connections, active_unique_ips,
       connection_links, max_tcp_conns, max_unique_ips,
       data_quota_bytes, expiration,
       discovered_at, updated_at
FROM discovered_clients
WHERE agent_id = $1
ORDER BY discovered_at DESC, id ASC`

	rows, err := r.db.QueryContext(ctx, q, agentID)
	if err != nil {
		return nil, fmt.Errorf("discoveredRepository.ListByAgent: %w", err)
	}
	defer rows.Close()

	var out []discovered.DiscoveredClient
	for rows.Next() {
		var (
			dc             discovered.DiscoveredClient
			id             string
			agentIDCol     string
			status         string
			connectionJSON []byte
			discoveredAt   time.Time
			updatedAt      time.Time
		)
		if err := rows.Scan(
			&id, &agentIDCol, &dc.ClientName, &dc.Secret, &status,
			&dc.TotalOctets, &dc.CurrentConnections, &dc.ActiveUniqueIPs,
			&connectionJSON, &dc.MaxTCPConns, &dc.MaxUniqueIPs,
			&dc.DataQuotaBytes, &dc.Expiration,
			&discoveredAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("discoveredRepository.ListByAgent scan: %w", err)
		}
		dc.ID = discovered.DiscoveredID(id)
		dc.AgentID = agentIDCol
		dc.Status = discovered.Status(status)
		if len(connectionJSON) > 0 {
			_ = json.Unmarshal(connectionJSON, &dc.ConnectionLinks)
		}
		dc.FirstSeen = discoveredAt.UTC()
		dc.UpdatedAt = updatedAt.UTC()
		out = append(out, dc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("discoveredRepository.ListByAgent rows: %w", err)
	}
	return out, nil
}

func (r *discoveredRepository) Save(ctx context.Context, dc discovered.DiscoveredClient) error {
	if err := r.q.UpsertDiscoveredClient(ctx, discoveredClientToUpsertParams(dc)); err != nil {
		return fmt.Errorf("discoveredRepository.Save: %w", err)
	}
	return nil
}

func (r *discoveredRepository) UpdateStatus(ctx context.Context, id discovered.DiscoveredID, status discovered.Status, observedAt time.Time) error {
	n, err := r.q.UpdateDiscoveredClientStatus(ctx, dbsqlc.UpdateDiscoveredClientStatusParams{
		ID:        string(id),
		Status:    string(status),
		UpdatedAt: observedAt.UTC(),
	})
	if err != nil {
		return fmt.Errorf("discoveredRepository.UpdateStatus: %w", err)
	}
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// UpdateStatusBulk updates the status of multiple discovered clients.
// dbsqlc has no bulk variant, so we loop over UpdateDiscoveredClientStatus.
func (r *discoveredRepository) UpdateStatusBulk(ctx context.Context, ids []discovered.DiscoveredID, status discovered.Status, observedAt time.Time) error {
	for _, id := range ids {
		if err := r.UpdateStatus(ctx, id, status, observedAt); err != nil {
			return fmt.Errorf("discoveredRepository.UpdateStatusBulk id=%s: %w", id, err)
		}
	}
	return nil
}

func (r *discoveredRepository) Delete(ctx context.Context, id discovered.DiscoveredID) error {
	n, err := r.q.DeleteDiscoveredClient(ctx, string(id))
	if err != nil {
		return fmt.Errorf("discoveredRepository.Delete: %w", err)
	}
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mapping helpers
// ---------------------------------------------------------------------------

func pgGetRowToDiscoveredClient(row dbsqlc.GetDiscoveredClientRow) discovered.DiscoveredClient {
	dc := discovered.DiscoveredClient{
		ID:                 discovered.DiscoveredID(row.ID),
		AgentID:            row.AgentID,
		ClientName:         row.ClientName,
		Secret:             row.Secret,
		Status:             discovered.Status(row.Status),
		TotalOctets:        uint64(row.TotalOctets),        //nolint:gosec
		CurrentConnections: uint32(row.CurrentConnections), //nolint:gosec
		ActiveUniqueIPs:    uint32(row.ActiveUniqueIps),    //nolint:gosec
		MaxTCPConns:        int(row.MaxTcpConns),           //nolint:gosec
		MaxUniqueIPs:       int(row.MaxUniqueIps),          //nolint:gosec
		DataQuotaBytes:     row.DataQuotaBytes,
		Expiration:         row.Expiration,
		FirstSeen:          row.DiscoveredAt.UTC(),
		UpdatedAt:          row.UpdatedAt.UTC(),
	}
	if len(row.ConnectionLinks) > 0 {
		_ = json.Unmarshal(row.ConnectionLinks, &dc.ConnectionLinks)
	}
	return dc
}

func pgGetByAgentAndNameRowToDiscoveredClient(row dbsqlc.GetDiscoveredClientByAgentAndNameRow) discovered.DiscoveredClient {
	dc := discovered.DiscoveredClient{
		ID:                 discovered.DiscoveredID(row.ID),
		AgentID:            row.AgentID,
		ClientName:         row.ClientName,
		Secret:             row.Secret,
		Status:             discovered.Status(row.Status),
		TotalOctets:        uint64(row.TotalOctets),        //nolint:gosec
		CurrentConnections: uint32(row.CurrentConnections), //nolint:gosec
		ActiveUniqueIPs:    uint32(row.ActiveUniqueIps),    //nolint:gosec
		MaxTCPConns:        int(row.MaxTcpConns),           //nolint:gosec
		MaxUniqueIPs:       int(row.MaxUniqueIps),          //nolint:gosec
		DataQuotaBytes:     row.DataQuotaBytes,
		Expiration:         row.Expiration,
		FirstSeen:          row.DiscoveredAt.UTC(),
		UpdatedAt:          row.UpdatedAt.UTC(),
	}
	if len(row.ConnectionLinks) > 0 {
		_ = json.Unmarshal(row.ConnectionLinks, &dc.ConnectionLinks)
	}
	return dc
}

func pgListRowToDiscoveredClient(row dbsqlc.ListDiscoveredClientsRow) discovered.DiscoveredClient {
	dc := discovered.DiscoveredClient{
		ID:                 discovered.DiscoveredID(row.ID),
		AgentID:            row.AgentID,
		ClientName:         row.ClientName,
		Secret:             row.Secret,
		Status:             discovered.Status(row.Status),
		TotalOctets:        uint64(row.TotalOctets),        //nolint:gosec
		CurrentConnections: uint32(row.CurrentConnections), //nolint:gosec
		ActiveUniqueIPs:    uint32(row.ActiveUniqueIps),    //nolint:gosec
		MaxTCPConns:        int(row.MaxTcpConns),           //nolint:gosec
		MaxUniqueIPs:       int(row.MaxUniqueIps),          //nolint:gosec
		DataQuotaBytes:     row.DataQuotaBytes,
		Expiration:         row.Expiration,
		FirstSeen:          row.DiscoveredAt.UTC(),
		UpdatedAt:          row.UpdatedAt.UTC(),
	}
	if len(row.ConnectionLinks) > 0 {
		_ = json.Unmarshal(row.ConnectionLinks, &dc.ConnectionLinks)
	}
	return dc
}

func discoveredClientToUpsertParams(dc discovered.DiscoveredClient) dbsqlc.UpsertDiscoveredClientParams {
	p := dbsqlc.UpsertDiscoveredClientParams{
		ID:                 string(dc.ID),
		AgentID:            dc.AgentID,
		ClientName:         dc.ClientName,
		Secret:             dc.Secret,
		Status:             string(dc.Status),
		TotalOctets:        int64(dc.TotalOctets),        //nolint:gosec
		CurrentConnections: int32(dc.CurrentConnections), //nolint:gosec
		ActiveUniqueIps:    int32(dc.ActiveUniqueIPs),    //nolint:gosec
		MaxTcpConns:        int32(dc.MaxTCPConns),        //nolint:gosec
		MaxUniqueIps:       int32(dc.MaxUniqueIPs),       //nolint:gosec
		DataQuotaBytes:     dc.DataQuotaBytes,
		Expiration:         dc.Expiration,
		DiscoveredAt:       dc.FirstSeen.UTC(),
		UpdatedAt:          dc.UpdatedAt.UTC(),
	}
	if len(dc.ConnectionLinks) > 0 {
		if b, err := json.Marshal(dc.ConnectionLinks); err == nil {
			p.ConnectionLinks = b
		} else {
			p.ConnectionLinks = json.RawMessage("[]")
		}
	} else {
		p.ConnectionLinks = json.RawMessage("[]")
	}
	return p
}
