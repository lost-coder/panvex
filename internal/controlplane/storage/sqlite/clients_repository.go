// internal/controlplane/storage/sqlite/clients_repository.go
//
// clients.Repository implementation backed by SQLite via direct database/sql
// queries. Mirrors the Postgres implementation (storage/postgres/clients_repository.go)
// but uses ? placeholders and SQLite-specific type handling (INTEGER unix
// timestamps, JSON-as-TEXT, integer booleans).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// clientsRepository implements clients.Repository against SQLite.
// db satisfies dbtx which is implemented by both *sql.DB and *sql.Tx,
// enabling the same code to run inside or outside a transaction.
type clientsRepository struct {
	db  dbtx
	raw *sql.DB // non-nil only when db is a *sql.DB pool; used for bulk tx
}

// dbtx abstracts what clientsRepository needs from the SQLite backend so
// the same code works for *sql.DB (pool) and *sql.Tx (transaction).
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// NewClientsRepository wires a clients.Repository against a SQLite
// connection or transaction. Accepts *sql.DB (pool) or *sql.Tx.
// When called with a *Store, use store.DB() to pass the underlying *sql.DB.
func NewClientsRepository(db dbtx) clients.Repository {
	raw, _ := db.(*sql.DB)
	return &clientsRepository{db: db, raw: raw}
}

// ---------------------------------------------------------------------------
// Client ops
// ---------------------------------------------------------------------------

func (r *clientsRepository) Get(ctx context.Context, id clients.ClientID) (clients.Client, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT
			id, name, secret_ciphertext, user_ad_tag, enabled,
			max_tcp_conns, max_unique_ips, data_quota_bytes,
			expiration_rfc3339, subscription_token,
			created_at_unix, updated_at_unix, deleted_at_unix
		FROM clients
		WHERE id = ? AND deleted_at_unix IS NULL
	`, string(id))
	c, err := scanClient(row)
	if errors.Is(err, storage.ErrNotFound) {
		return clients.Client{}, storage.ErrNotFound
	}
	if err != nil {
		return clients.Client{}, fmt.Errorf("clientsRepository.Get: %w", err)
	}
	return c, nil
}

func (r *clientsRepository) GetBySubscriptionToken(ctx context.Context, token string) (clients.Client, error) {
	if token == "" {
		return clients.Client{}, storage.ErrNotFound
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT
			id, name, secret_ciphertext, user_ad_tag, enabled,
			max_tcp_conns, max_unique_ips, data_quota_bytes,
			expiration_rfc3339, subscription_token,
			created_at_unix, updated_at_unix, deleted_at_unix
		FROM clients
		WHERE subscription_token = ? AND deleted_at_unix IS NULL
	`, token)
	c, err := scanClient(row)
	if errors.Is(err, storage.ErrNotFound) {
		return clients.Client{}, storage.ErrNotFound
	}
	if err != nil {
		return clients.Client{}, fmt.Errorf("clientsRepository.GetBySubscriptionToken: %w", err)
	}
	return c, nil
}

func (r *clientsRepository) List(ctx context.Context) ([]clients.Client, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			id, name, secret_ciphertext, user_ad_tag, enabled,
			max_tcp_conns, max_unique_ips, data_quota_bytes,
			expiration_rfc3339, subscription_token,
			created_at_unix, updated_at_unix, deleted_at_unix
		FROM clients
		WHERE deleted_at_unix IS NULL
		ORDER BY created_at_unix, id
	`)
	if err != nil {
		return nil, fmt.Errorf("clientsRepository.List: %w", err)
	}
	defer rows.Close()

	var out []clients.Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, fmt.Errorf("clientsRepository.List scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clientsRepository.List rows: %w", err)
	}
	if out == nil {
		out = []clients.Client{}
	}
	return out, nil
}

func (r *clientsRepository) Save(ctx context.Context, c clients.Client) error {
	var deletedAt sql.NullInt64
	if c.DeletedAt != nil {
		deletedAt.Valid = true
		deletedAt.Int64 = toUnix(*c.DeletedAt)
	}
	subscriptionToken := sql.NullString{String: c.SubscriptionToken, Valid: c.SubscriptionToken != ""}

	var returnedID string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO clients (
			id, name, secret_ciphertext, user_ad_tag, enabled,
			max_tcp_conns, max_unique_ips, data_quota_bytes,
			expiration_rfc3339, subscription_token,
			created_at_unix, updated_at_unix, deleted_at_unix
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name               = excluded.name,
			secret_ciphertext  = excluded.secret_ciphertext,
			user_ad_tag        = excluded.user_ad_tag,
			enabled            = excluded.enabled,
			max_tcp_conns      = excluded.max_tcp_conns,
			max_unique_ips     = excluded.max_unique_ips,
			data_quota_bytes   = excluded.data_quota_bytes,
			expiration_rfc3339 = excluded.expiration_rfc3339,
			subscription_token = excluded.subscription_token,
			-- M-3: preserve the original created_at on update to match the
			-- Postgres UpsertClient contract (which does not touch created_at
			-- on conflict). SQLite previously overwrote it from the incoming
			-- record, mutating a client's creation time on every save.
			-- deleted_at_unix intentionally tracks the incoming record: the
			-- SQLite delete flow soft-deletes by saving a tombstoned record,
			-- while an active-client save carries nil (clearing it).
			updated_at_unix    = excluded.updated_at_unix,
			deleted_at_unix    = excluded.deleted_at_unix
		RETURNING id
	`,
		string(c.ID), c.Name, c.Secret, c.UserADTag, boolToInt(c.Enabled),
		c.MaxTCPConns, c.MaxUniqueIPs, c.DataQuotaBytes, c.ExpirationRFC3339, subscriptionToken,
		toUnix(c.CreatedAt), toUnix(c.UpdatedAt), deletedAt,
	).Scan(&returnedID)
	if err != nil {
		return fmt.Errorf("clientsRepository.Save: %w", err)
	}
	if returnedID != string(c.ID) {
		return fmt.Errorf("clientsRepository.Save: upsert returned id %q, want %q", returnedID, c.ID)
	}
	return nil
}

// Delete soft-deletes the client row and then explicitly removes all child
// rows so ListAssignments / ListDeployments / ListUsage observe the deletion
// immediately. Soft-delete does not trigger ON DELETE CASCADE because the
// client row still exists.
//
// Delete is NOT atomic on its own — callers must invoke it within a
// transaction (uow.Do / Store.Transact) so partial progress is not visible
// on crash.
func (r *clientsRepository) Delete(ctx context.Context, id clients.ClientID) error {
	now := toUnix(time.Now().UTC())
	result, err := r.db.ExecContext(ctx, `
		UPDATE clients SET deleted_at_unix = ? WHERE id = ? AND deleted_at_unix IS NULL
	`, now, string(id))
	if err != nil {
		return fmt.Errorf("clientsRepository.Delete: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("clientsRepository.Delete rows: %w", err)
	}
	if n == 0 {
		return storage.ErrNotFound
	}

	if err := r.deleteAssignmentsRaw(ctx, string(id)); err != nil {
		return fmt.Errorf("clientsRepository.Delete (assignments): %w", err)
	}
	if err := r.deleteDeploymentsRaw(ctx, string(id)); err != nil {
		return fmt.Errorf("clientsRepository.Delete (deployments): %w", err)
	}
	if err := r.deleteUsageRaw(ctx, string(id)); err != nil {
		return fmt.Errorf("clientsRepository.Delete (usage): %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Assignment ops
// ---------------------------------------------------------------------------

func (r *clientsRepository) ListAssignments(ctx context.Context, clientID clients.ClientID) ([]clients.Assignment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, client_id, target_type, fleet_group_id, agent_id, created_at_unix
		FROM client_assignments
		WHERE client_id = ?
		ORDER BY created_at_unix, id
	`, string(clientID))
	if err != nil {
		return nil, fmt.Errorf("clientsRepository.ListAssignments: %w", err)
	}
	defer rows.Close()

	var out []clients.Assignment
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, fmt.Errorf("clientsRepository.ListAssignments scan: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clientsRepository.ListAssignments rows: %w", err)
	}
	if out == nil {
		out = []clients.Assignment{}
	}
	return out, nil
}

// SaveAssignments replaces the full assignment set for clientID: delete all
// existing rows then insert the new ones.
//
// SaveAssignments is NOT atomic on its own — callers must invoke it within a
// transaction (uow.Do / Store.Transact) so partial progress is not visible
// on crash.
func (r *clientsRepository) SaveAssignments(ctx context.Context, clientID clients.ClientID, assignments []clients.Assignment) error {
	if err := r.deleteAssignmentsRaw(ctx, string(clientID)); err != nil {
		return fmt.Errorf("clientsRepository.SaveAssignments (delete): %w", err)
	}
	for _, a := range assignments {
		if err := r.insertAssignment(ctx, a); err != nil {
			return fmt.Errorf("clientsRepository.SaveAssignments (insert %s): %w", a.ID, err)
		}
	}
	return nil
}

func (r *clientsRepository) DeleteAssignments(ctx context.Context, clientID clients.ClientID) error {
	if err := r.deleteAssignmentsRaw(ctx, string(clientID)); err != nil {
		return fmt.Errorf("clientsRepository.DeleteAssignments: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Deployment ops
// ---------------------------------------------------------------------------

func (r *clientsRepository) ListDeployments(ctx context.Context, clientID clients.ClientID) ([]clients.Deployment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT client_id, agent_id, desired_operation, status, last_error,
			connection_links, link_diagnostic, last_applied_at_unix, updated_at_unix,
			last_reset_epoch_secs
		FROM client_deployments
		WHERE client_id = ?
		ORDER BY agent_id
	`, string(clientID))
	if err != nil {
		return nil, fmt.Errorf("clientsRepository.ListDeployments: %w", err)
	}
	defer rows.Close()

	var out []clients.Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, fmt.Errorf("clientsRepository.ListDeployments scan: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clientsRepository.ListDeployments rows: %w", err)
	}
	if out == nil {
		out = []clients.Deployment{}
	}
	return out, nil
}

// SaveDeployments replaces the full deployment set for clientID: delete all
// existing rows then upsert the new ones.
//
// SaveDeployments is NOT atomic on its own — callers must invoke it within a
// transaction (uow.Do / Store.Transact) so partial progress is not visible
// on crash.
func (r *clientsRepository) SaveDeployments(ctx context.Context, clientID clients.ClientID, deployments []clients.Deployment) error {
	if err := r.deleteDeploymentsRaw(ctx, string(clientID)); err != nil {
		return fmt.Errorf("clientsRepository.SaveDeployments (delete): %w", err)
	}
	for _, d := range deployments {
		if err := r.upsertDeployment(ctx, d); err != nil {
			return fmt.Errorf("clientsRepository.SaveDeployments (upsert %s/%s): %w", d.ClientID, d.AgentID, err)
		}
	}
	return nil
}

// PutDeployment upserts a single deployment row (client_id, agent_id natural key).
// This is the Repository-backed replacement for the legacy ClientStore.PutClientDeployment.
func (r *clientsRepository) PutDeployment(ctx context.Context, d clients.Deployment) error {
	if err := r.upsertDeployment(ctx, d); err != nil {
		return fmt.Errorf("clientsRepository.PutDeployment (%s/%s): %w", d.ClientID, d.AgentID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Usage ops
// ---------------------------------------------------------------------------

func (r *clientsRepository) UpsertUsage(ctx context.Context, u clients.Usage) error {
	if err := r.upsertUsageRow(ctx, r.db, u); err != nil {
		return fmt.Errorf("clientsRepository.UpsertUsage: %w", err)
	}
	return nil
}

// UpsertUsageBulk upserts a batch of usage rows. Uses the existing Store bulk
// path (execInTx + chunked INSERT) when the repository holds a real *sql.DB
// pool; falls back to per-row upserts when running inside a transaction.
func (r *clientsRepository) UpsertUsageBulk(ctx context.Context, batch []clients.Usage) error {
	if len(batch) == 0 {
		return nil
	}

	// Fast path: have a pool — use chunked bulk INSERT inside a transaction.
	if r.raw != nil {
		return r.bulkUpsertUsage(ctx, batch)
	}

	// Slow path: already inside a transaction — fall back to per-row.
	for _, u := range batch {
		if err := r.upsertUsageRow(ctx, r.db, u); err != nil {
			return fmt.Errorf("clientsRepository.UpsertUsageBulk (client=%s agent=%s): %w", u.ClientID, u.AgentID, err)
		}
	}
	return nil
}

func (r *clientsRepository) ListUsage(ctx context.Context) ([]clients.Usage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT client_id, agent_id, traffic_used_bytes, unique_ips_used,
			active_tcp_conns, active_unique_ips, quota_used_bytes,
			quota_last_reset_unix, observed_at_unix,
			agent_boot_id, last_total_bytes
		FROM client_usage
	`)
	if err != nil {
		return nil, fmt.Errorf("clientsRepository.ListUsage: %w", err)
	}
	defer rows.Close()

	var out []clients.Usage
	for rows.Next() {
		u, err := scanUsage(rows)
		if err != nil {
			return nil, fmt.Errorf("clientsRepository.ListUsage scan: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clientsRepository.ListUsage rows: %w", err)
	}
	if out == nil {
		out = []clients.Usage{}
	}
	return out, nil
}

func (r *clientsRepository) DeleteUsageByClient(ctx context.Context, id clients.ClientID) error {
	if err := r.deleteUsageRaw(ctx, string(id)); err != nil {
		return fmt.Errorf("clientsRepository.DeleteUsageByClient: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Private helpers — raw SQL operations
// ---------------------------------------------------------------------------

func (r *clientsRepository) deleteAssignmentsRaw(ctx context.Context, clientID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM client_assignments WHERE client_id = ?`, clientID)
	return err
}

func (r *clientsRepository) deleteDeploymentsRaw(ctx context.Context, clientID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM client_deployments WHERE client_id = ?`, clientID)
	return err
}

func (r *clientsRepository) deleteUsageRaw(ctx context.Context, clientID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM client_usage WHERE client_id = ?`, clientID)
	return err
}

func (r *clientsRepository) insertAssignment(ctx context.Context, a clients.Assignment) error {
	var fleetGroupID sql.NullString
	if string(a.FleetGroupID) != "" {
		fleetGroupID = sql.NullString{String: string(a.FleetGroupID), Valid: true}
	}
	var agentID sql.NullString
	if a.AgentID != "" {
		agentID = sql.NullString{String: a.AgentID, Valid: true}
	}

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO client_assignments (id, client_id, target_type, fleet_group_id, agent_id, created_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			client_id      = excluded.client_id,
			target_type    = excluded.target_type,
			fleet_group_id = excluded.fleet_group_id,
			agent_id       = excluded.agent_id,
			created_at_unix = excluded.created_at_unix
	`, string(a.ID), string(a.ClientID), a.TargetType, fleetGroupID, agentID, toUnix(a.CreatedAt))
	return err
}

func (r *clientsRepository) upsertDeployment(ctx context.Context, d clients.Deployment) error {
	var lastAppliedAt sql.NullInt64
	if d.LastAppliedAt != nil {
		lastAppliedAt = sql.NullInt64{Int64: toUnix(*d.LastAppliedAt), Valid: true}
	}

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO client_deployments (
			client_id, agent_id, desired_operation, status, last_error,
			connection_links, link_diagnostic, last_applied_at_unix, updated_at_unix,
			last_reset_epoch_secs
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(client_id, agent_id) DO UPDATE SET
			desired_operation     = excluded.desired_operation,
			status                = excluded.status,
			last_error            = excluded.last_error,
			connection_links      = excluded.connection_links,
			link_diagnostic       = excluded.link_diagnostic,
			last_applied_at_unix  = excluded.last_applied_at_unix,
			updated_at_unix       = excluded.updated_at_unix,
			last_reset_epoch_secs = excluded.last_reset_epoch_secs
	`,
		string(d.ClientID), d.AgentID, d.DesiredOperation, d.Status, d.LastError,
		encodeStringArray(d.ConnectionLinks), d.LinkDiagnostic, lastAppliedAt, toUnix(d.UpdatedAt),
		int64(d.LastResetEpochSecs), //nolint:gosec
	)
	return err
}

// upsertUsageRow inserts or updates one (client, agent) usage row. last_seq
// is the agent's per-connection report cursor; the ON CONFLICT DO UPDATE
// only fires when the incoming last_seq is strictly newer than the stored
// one, so an out-of-order or duplicate/older report is a no-op rather than
// regressing the stored counters (audit finding: monotonicity guard). A
// brand-new (client, agent) pair always inserts normally since ON CONFLICT
// only triggers against an existing row.
func (r *clientsRepository) upsertUsageRow(ctx context.Context, exec dbtx, u clients.Usage) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO client_usage (
			client_id, agent_id, traffic_used_bytes, unique_ips_used,
			active_tcp_conns, active_unique_ips, quota_used_bytes,
			quota_last_reset_unix, observed_at_unix,
			agent_boot_id, last_total_bytes
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(client_id, agent_id) DO UPDATE SET
			traffic_used_bytes    = excluded.traffic_used_bytes,
			unique_ips_used       = excluded.unique_ips_used,
			active_tcp_conns      = excluded.active_tcp_conns,
			active_unique_ips     = excluded.active_unique_ips,
			quota_used_bytes      = excluded.quota_used_bytes,
			quota_last_reset_unix = excluded.quota_last_reset_unix,
			observed_at_unix      = excluded.observed_at_unix,
			agent_boot_id         = excluded.agent_boot_id,
			last_total_bytes      = excluded.last_total_bytes
	`,
		string(u.ClientID), u.AgentID,
		int64(u.TrafficUsedBytes), u.UniqueIPsUsed, //nolint:gosec
		u.ActiveTCPConns, u.ActiveUniqueIPs,
		int64(u.QuotaUsedBytes), int64(u.QuotaLastResetUnix), //nolint:gosec
		toUnix(u.ObservedAt),
		u.AgentBootID, int64(u.LastTotalBytes), //nolint:gosec
	)
	return err
}

// bulkUpsertUsage runs chunked INSERT for the usage batch inside a single
// BEGIN IMMEDIATE transaction. Reuses rowPlaceholders from bulk.go.
// Unconditional last-write-wins upsert (P4): ordering/duplicate protection
// lives upstream in the panel's watermark derivation. SQLite applies each
// VALUES row in the statement in order, so within-batch duplicates for the
// same (client, agent) collapse to the trailing entry.
func (r *clientsRepository) bulkUpsertUsage(ctx context.Context, batch []clients.Usage) error {
	conn, err := r.raw.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() { //nolint:contextcheck // deferred cleanup must outlive caller ctx
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	const cols = 11
	exec := connExecutor{conn: conn}
	for start := 0; start < len(batch); start += bulkChunkSize {
		end := start + bulkChunkSize
		if end > len(batch) {
			end = len(batch)
		}
		chunk := batch[start:end]
		args := make([]any, 0, len(chunk)*cols)
		for _, u := range chunk {
			args = append(args,
				string(u.ClientID), u.AgentID,
				int64(u.TrafficUsedBytes), u.UniqueIPsUsed, //nolint:gosec
				u.ActiveTCPConns, u.ActiveUniqueIPs,
				int64(u.QuotaUsedBytes), int64(u.QuotaLastResetUnix), //nolint:gosec
				toUnix(u.ObservedAt),
				u.AgentBootID, int64(u.LastTotalBytes), //nolint:gosec
			)
		}
		query := fmt.Sprintf(`
			INSERT INTO client_usage (
				client_id, agent_id, traffic_used_bytes, unique_ips_used,
				active_tcp_conns, active_unique_ips, quota_used_bytes,
				quota_last_reset_unix, observed_at_unix,
				agent_boot_id, last_total_bytes
			) VALUES %s
			ON CONFLICT(client_id, agent_id) DO UPDATE SET
				traffic_used_bytes    = excluded.traffic_used_bytes,
				unique_ips_used       = excluded.unique_ips_used,
				active_tcp_conns      = excluded.active_tcp_conns,
				active_unique_ips     = excluded.active_unique_ips,
				quota_used_bytes      = excluded.quota_used_bytes,
				quota_last_reset_unix = excluded.quota_last_reset_unix,
				observed_at_unix      = excluded.observed_at_unix,
				agent_boot_id         = excluded.agent_boot_id,
				last_total_bytes      = excluded.last_total_bytes`,
			rowPlaceholders(len(chunk), cols))
		if _, err := exec.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

// ---------------------------------------------------------------------------
// Scan helpers
// ---------------------------------------------------------------------------

type clientScanner interface {
	Scan(dest ...any) error
}

func scanClient(s clientScanner) (clients.Client, error) {
	var (
		c                 clients.Client
		id                string
		enabled           int
		subscriptionToken sql.NullString
		createdAt         int64
		updatedAt         int64
		deletedAt         sql.NullInt64
	)
	if err := s.Scan(
		&id, &c.Name, &c.Secret, &c.UserADTag, &enabled,
		&c.MaxTCPConns, &c.MaxUniqueIPs, &c.DataQuotaBytes,
		&c.ExpirationRFC3339, &subscriptionToken,
		&createdAt, &updatedAt, &deletedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clients.Client{}, storage.ErrNotFound
		}
		return clients.Client{}, err
	}
	c.ID = clients.ClientID(id)
	c.Enabled = intToBool(enabled)
	c.SubscriptionToken = subscriptionToken.String // NULL → ""
	c.CreatedAt = fromUnix(createdAt)
	c.UpdatedAt = fromUnix(updatedAt)
	if deletedAt.Valid {
		t := fromUnix(deletedAt.Int64)
		c.DeletedAt = &t
	}
	return c, nil
}

type assignmentScanner interface {
	Scan(dest ...any) error
}

func scanAssignment(s assignmentScanner) (clients.Assignment, error) {
	var (
		a            clients.Assignment
		id           string
		clientID     string
		fleetGroupID sql.NullString
		agentID      sql.NullString
		createdAt    int64
	)
	if err := s.Scan(&id, &clientID, &a.TargetType, &fleetGroupID, &agentID, &createdAt); err != nil {
		return clients.Assignment{}, err
	}
	a.ID = clients.AssignmentID(id)
	a.ClientID = clients.ClientID(clientID)
	if fleetGroupID.Valid {
		a.FleetGroupID = clients.FleetGroupID(fleetGroupID.String)
	}
	if agentID.Valid {
		a.AgentID = agentID.String
	}
	a.CreatedAt = fromUnix(createdAt)
	return a, nil
}

type deploymentScanner interface {
	Scan(dest ...any) error
}

func scanDeployment(s deploymentScanner) (clients.Deployment, error) {
	var (
		d           clients.Deployment
		clientID    string
		linksJSON   string
		lastApplied sql.NullInt64
		updatedAt   int64
		lastReset   int64
	)
	if err := s.Scan(
		&clientID, &d.AgentID, &d.DesiredOperation, &d.Status, &d.LastError,
		&linksJSON, &d.LinkDiagnostic, &lastApplied, &updatedAt, &lastReset,
	); err != nil {
		return clients.Deployment{}, err
	}
	d.ClientID = clients.ClientID(clientID)
	d.ConnectionLinks = decodeStringArray(linksJSON)
	if lastApplied.Valid {
		t := fromUnix(lastApplied.Int64)
		d.LastAppliedAt = &t
	}
	d.UpdatedAt = fromUnix(updatedAt)
	d.LastResetEpochSecs = uint64(lastReset) //nolint:gosec
	return d, nil
}

type usageScanner interface {
	Scan(dest ...any) error
}

func scanUsage(s usageScanner) (clients.Usage, error) {
	var (
		u              clients.Usage
		clientID       string
		traffic        int64
		quotaUsed      int64
		quotaLastReset int64
		observedAt     int64
		lastTotal      int64
	)
	if err := s.Scan(
		&clientID, &u.AgentID, &traffic, &u.UniqueIPsUsed,
		&u.ActiveTCPConns, &u.ActiveUniqueIPs, &quotaUsed, &quotaLastReset,
		&observedAt, &u.AgentBootID, &lastTotal,
	); err != nil {
		return clients.Usage{}, err
	}
	u.ClientID = clients.ClientID(clientID)
	u.TrafficUsedBytes = uint64(traffic)          //nolint:gosec
	u.QuotaUsedBytes = uint64(quotaUsed)          //nolint:gosec
	u.QuotaLastResetUnix = uint64(quotaLastReset) //nolint:gosec
	u.LastTotalBytes = uint64(lastTotal)          //nolint:gosec
	u.ObservedAt = fromUnix(observedAt)
	return u, nil
}
