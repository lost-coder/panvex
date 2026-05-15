// internal/controlplane/storage/postgres/clients_repository.go
//
// clients.Repository implementation backed by Postgres via dbsqlc.
// This is the authoritative persistence layer for the clients domain;
// it replaces the ad-hoc raw-SQL methods on Store for new callers.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// clientsRepository implements clients.Repository against Postgres via
// the dbsqlc query layer.
type clientsRepository struct {
	q *dbsqlc.Queries
}

// NewClientsRepository wires a clients.Repository against a Postgres
// connection or transaction. db may be *sql.DB (pool) or *sql.Tx.
func NewClientsRepository(db dbsqlc.DBTX) clients.Repository {
	return &clientsRepository{q: dbsqlc.New(db)}
}

// ---------------------------------------------------------------------------
// Client ops
// ---------------------------------------------------------------------------

func (r *clientsRepository) Get(ctx context.Context, id clients.ClientID) (clients.Client, error) {
	row, err := r.q.GetClient(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return clients.Client{}, storage.ErrNotFound
	}
	if err != nil {
		return clients.Client{}, fmt.Errorf("clientsRepository.Get: %w", err)
	}
	return rowToClient(row), nil
}

func (r *clientsRepository) List(ctx context.Context) ([]clients.Client, error) {
	rows, err := r.q.ListClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("clientsRepository.List: %w", err)
	}
	out := make([]clients.Client, len(rows))
	for i, row := range rows {
		out[i] = rowToClient(row)
	}
	return out, nil
}

func (r *clientsRepository) Save(ctx context.Context, c clients.Client) error {
	if err := r.q.UpsertClient(ctx, clientToUpsertParams(c)); err != nil {
		return fmt.Errorf("clientsRepository.Save: %w", err)
	}
	return nil
}

func (r *clientsRepository) Delete(ctx context.Context, id clients.ClientID) error {
	now := time.Now().UTC()
	arg := dbsqlc.SoftDeleteClientParams{
		ID:        string(id),
		DeletedAt: sql.NullTime{Time: now, Valid: true},
	}
	n, err := r.q.SoftDeleteClient(ctx, arg)
	if err != nil {
		return fmt.Errorf("clientsRepository.Delete: %w", err)
	}
	if n == 0 {
		return storage.ErrNotFound
	}
	// Explicitly remove child rows so ListAssignments / ListDeployments /
	// ListUsage observe the deletion immediately (soft-delete does not
	// trigger the ON DELETE CASCADE FK on child tables).
	if err := r.q.DeleteClientAssignmentsForClient(ctx, string(id)); err != nil {
		return fmt.Errorf("clientsRepository.Delete (assignments): %w", err)
	}
	if err := r.q.DeleteClientDeploymentsForClient(ctx, string(id)); err != nil {
		return fmt.Errorf("clientsRepository.Delete (deployments): %w", err)
	}
	if err := r.q.DeleteClientUsageByClient(ctx, string(id)); err != nil {
		return fmt.Errorf("clientsRepository.Delete (usage): %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Assignment ops
// ---------------------------------------------------------------------------

func (r *clientsRepository) ListAssignments(ctx context.Context, clientID clients.ClientID) ([]clients.Assignment, error) {
	rows, err := r.q.ListClientAssignments(ctx, string(clientID))
	if err != nil {
		return nil, fmt.Errorf("clientsRepository.ListAssignments: %w", err)
	}
	out := make([]clients.Assignment, len(rows))
	for i, row := range rows {
		out[i] = rowToAssignment(row)
	}
	return out, nil
}

// SaveAssignments replaces the full assignment set for clientID: delete
// all existing rows then insert the new ones.
func (r *clientsRepository) SaveAssignments(ctx context.Context, clientID clients.ClientID, assignments []clients.Assignment) error {
	if err := r.q.DeleteClientAssignmentsForClient(ctx, string(clientID)); err != nil {
		return fmt.Errorf("clientsRepository.SaveAssignments (delete): %w", err)
	}
	for _, a := range assignments {
		if err := r.q.InsertClientAssignment(ctx, assignmentToInsertParams(a)); err != nil {
			return fmt.Errorf("clientsRepository.SaveAssignments (insert %s): %w", a.ID, err)
		}
	}
	return nil
}

func (r *clientsRepository) DeleteAssignments(ctx context.Context, clientID clients.ClientID) error {
	if err := r.q.DeleteClientAssignmentsForClient(ctx, string(clientID)); err != nil {
		return fmt.Errorf("clientsRepository.DeleteAssignments: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Deployment ops
// ---------------------------------------------------------------------------

func (r *clientsRepository) ListDeployments(ctx context.Context, clientID clients.ClientID) ([]clients.Deployment, error) {
	rows, err := r.q.ListClientDeployments(ctx, string(clientID))
	if err != nil {
		return nil, fmt.Errorf("clientsRepository.ListDeployments: %w", err)
	}
	out := make([]clients.Deployment, len(rows))
	for i, row := range rows {
		out[i] = rowToDeployment(row)
	}
	return out, nil
}

// SaveDeployments replaces the full deployment set for clientID: delete
// all existing rows then insert the new ones.
func (r *clientsRepository) SaveDeployments(ctx context.Context, clientID clients.ClientID, deployments []clients.Deployment) error {
	if err := r.q.DeleteClientDeploymentsForClient(ctx, string(clientID)); err != nil {
		return fmt.Errorf("clientsRepository.SaveDeployments (delete): %w", err)
	}
	for _, d := range deployments {
		if err := r.q.UpsertClientDeployment(ctx, deploymentToUpsertParams(d)); err != nil {
			return fmt.Errorf("clientsRepository.SaveDeployments (upsert %s/%s): %w", d.ClientID, d.AgentID, err)
		}
	}
	return nil
}

// PutDeployment upserts a single deployment row (client_id, agent_id natural key).
// This is the Repository-backed replacement for the legacy ClientStore.PutClientDeployment.
func (r *clientsRepository) PutDeployment(ctx context.Context, d clients.Deployment) error {
	if err := r.q.UpsertClientDeployment(ctx, deploymentToUpsertParams(d)); err != nil {
		return fmt.Errorf("clientsRepository.PutDeployment (%s/%s): %w", d.ClientID, d.AgentID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Usage ops
// ---------------------------------------------------------------------------

func (r *clientsRepository) UpsertUsage(ctx context.Context, u clients.Usage) error {
	if err := r.q.UpsertClientUsage(ctx, usageToUpsertParams(u)); err != nil {
		return fmt.Errorf("clientsRepository.UpsertUsage: %w", err)
	}
	return nil
}

func (r *clientsRepository) UpsertUsageBulk(ctx context.Context, batch []clients.Usage) error {
	for _, u := range batch {
		if err := r.q.UpsertClientUsage(ctx, usageToUpsertParams(u)); err != nil {
			return fmt.Errorf("clientsRepository.UpsertUsageBulk (client=%s agent=%s): %w", u.ClientID, u.AgentID, err)
		}
	}
	return nil
}

func (r *clientsRepository) ListUsage(ctx context.Context) ([]clients.Usage, error) {
	rows, err := r.q.ListAllClientUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("clientsRepository.ListUsage: %w", err)
	}
	out := make([]clients.Usage, len(rows))
	for i, row := range rows {
		out[i] = rowToUsage(row)
	}
	return out, nil
}

func (r *clientsRepository) DeleteUsageByClient(ctx context.Context, id clients.ClientID) error {
	if err := r.q.DeleteClientUsageByClient(ctx, string(id)); err != nil {
		return fmt.Errorf("clientsRepository.DeleteUsageByClient: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mapping helpers — dbsqlc row → domain type
// ---------------------------------------------------------------------------

func rowToClient(row dbsqlc.Client) clients.Client {
	c := clients.Client{
		ID:                clients.ClientID(row.ID),
		Name:              row.Name,
		Secret:            row.SecretCiphertext,
		UserADTag:         row.UserAdTag,
		Enabled:           row.Enabled,
		MaxTCPConns:       int(row.MaxTcpConns),
		MaxUniqueIPs:      int(row.MaxUniqueIps),
		DataQuotaBytes:    row.DataQuotaBytes,
		ExpirationRFC3339: row.ExpirationRfc3339,
		CreatedAt:         row.CreatedAt.UTC(),
		UpdatedAt:         row.UpdatedAt.UTC(),
	}
	if row.DeletedAt.Valid {
		t := row.DeletedAt.Time.UTC()
		c.DeletedAt = &t
	}
	return c
}

func clientToUpsertParams(c clients.Client) dbsqlc.UpsertClientParams {
	return dbsqlc.UpsertClientParams{
		ID:                string(c.ID),
		Name:              c.Name,
		SecretCiphertext:  c.Secret,
		UserAdTag:         c.UserADTag,
		Enabled:           c.Enabled,
		MaxTcpConns:       int64(c.MaxTCPConns),
		MaxUniqueIps:      int64(c.MaxUniqueIPs),
		DataQuotaBytes:    c.DataQuotaBytes,
		ExpirationRfc3339: c.ExpirationRFC3339,
		CreatedAt:         c.CreatedAt.UTC(),
		UpdatedAt:         c.UpdatedAt.UTC(),
	}
}

// ---------------------------------------------------------------------------
// Mapping helpers — Assignment
// ---------------------------------------------------------------------------

func rowToAssignment(row dbsqlc.ClientAssignment) clients.Assignment {
	a := clients.Assignment{
		ID:         clients.AssignmentID(row.ID),
		ClientID:   clients.ClientID(row.ClientID),
		TargetType: row.TargetType,
		CreatedAt:  row.CreatedAt.UTC(),
	}
	if row.FleetGroupID.Valid {
		a.FleetGroupID = clients.FleetGroupID(row.FleetGroupID.UUID.String())
	}
	if row.AgentID.Valid {
		a.AgentID = row.AgentID.String
	}
	return a
}

func assignmentToInsertParams(a clients.Assignment) dbsqlc.InsertClientAssignmentParams {
	p := dbsqlc.InsertClientAssignmentParams{
		ID:         string(a.ID),
		ClientID:   string(a.ClientID),
		TargetType: a.TargetType,
		CreatedAt:  a.CreatedAt.UTC(),
	}
	if string(a.FleetGroupID) != "" {
		if id, err := uuid.Parse(string(a.FleetGroupID)); err == nil {
			p.FleetGroupID = uuid.NullUUID{UUID: id, Valid: true}
		}
	}
	if a.AgentID != "" {
		p.AgentID = sql.NullString{String: a.AgentID, Valid: true}
	}
	return p
}

// ---------------------------------------------------------------------------
// Mapping helpers — Deployment
// ---------------------------------------------------------------------------

func rowToDeployment(row dbsqlc.ListClientDeploymentsRow) clients.Deployment {
	d := clients.Deployment{
		ClientID:           clients.ClientID(row.ClientID),
		AgentID:            row.AgentID,
		DesiredOperation:   row.DesiredOperation,
		Status:             row.Status,
		LastError:          row.LastError,
		UpdatedAt:          row.UpdatedAt.UTC(),
		LastResetEpochSecs: uint64(row.LastResetEpochSecs), //nolint:gosec
	}
	if row.LastAppliedAt.Valid {
		t := row.LastAppliedAt.Time.UTC()
		d.LastAppliedAt = &t
	}
	if len(row.ConnectionLinks) > 0 {
		_ = json.Unmarshal(row.ConnectionLinks, &d.ConnectionLinks)
	}
	return d
}

func deploymentToUpsertParams(d clients.Deployment) dbsqlc.UpsertClientDeploymentParams {
	p := dbsqlc.UpsertClientDeploymentParams{
		ClientID:           string(d.ClientID),
		AgentID:            d.AgentID,
		DesiredOperation:   d.DesiredOperation,
		Status:             d.Status,
		LastError:          d.LastError,
		UpdatedAt:          d.UpdatedAt.UTC(),
		LastResetEpochSecs: int64(d.LastResetEpochSecs), //nolint:gosec
	}
	if d.LastAppliedAt != nil {
		p.LastAppliedAt = sql.NullTime{Time: d.LastAppliedAt.UTC(), Valid: true}
	}
	if len(d.ConnectionLinks) > 0 {
		if b, err := json.Marshal(d.ConnectionLinks); err == nil {
			p.ConnectionLinks = b
		}
	} else {
		p.ConnectionLinks = json.RawMessage("[]")
	}
	return p
}

// ---------------------------------------------------------------------------
// Mapping helpers — Usage
// ---------------------------------------------------------------------------

func rowToUsage(row dbsqlc.ClientUsage) clients.Usage {
	return clients.Usage{
		ClientID:         clients.ClientID(row.ClientID),
		AgentID:          row.AgentID,
		TrafficUsedBytes: uint64(row.TrafficUsedBytes), //nolint:gosec
		UniqueIPsUsed:    int(row.UniqueIpsUsed),
		ActiveTCPConns:   int(row.ActiveTcpConns),
		ActiveUniqueIPs:  int(row.ActiveUniqueIps),
		LastSeq:          uint64(row.LastSeq), //nolint:gosec
		ObservedAt:       row.ObservedAt.UTC(),
	}
}

func usageToUpsertParams(u clients.Usage) dbsqlc.UpsertClientUsageParams {
	return dbsqlc.UpsertClientUsageParams{
		ClientID:         string(u.ClientID),
		AgentID:          u.AgentID,
		TrafficUsedBytes: int64(u.TrafficUsedBytes), //nolint:gosec
		UniqueIpsUsed:    int32(u.UniqueIPsUsed),    //nolint:gosec
		ActiveTcpConns:   int32(u.ActiveTCPConns),   //nolint:gosec
		ActiveUniqueIps:  int32(u.ActiveUniqueIPs),  //nolint:gosec
		LastSeq:          int64(u.LastSeq),          //nolint:gosec
		ObservedAt:       u.ObservedAt.UTC(),
	}
}
