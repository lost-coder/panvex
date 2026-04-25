package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	_ "modernc.org/sqlite"
)

// sqlitePragmas are applied to every pooled connection via the modernc.org/sqlite
// `_pragma=` DSN parameter. See DF-17 / M-F10 in the remediation plan:
// without WAL + busy_timeout, any concurrent writer produces SQLITE_BUSY and
// bottles the SQLite deployment.
//
//   - journal_mode = WAL ........ Concurrent readers, serialized writers.
//     WAL is a database-level setting persisted in the file header; applying
//     it once upgrades the file permanently. Each connection still reports
//     `wal` when queried, which the tests rely on.
//   - synchronous = NORMAL ...... Recommended companion to WAL. Durable across
//     process crashes; a small window (last committed txn) is exposed to OS /
//     power loss. FULL is overkill under WAL because the WAL itself provides
//     crash-consistency for committed transactions. Accepted trade-off for
//     the control-plane workload — writes are idempotent / re-replayable.
//   - busy_timeout = 5000 ....... 5-second retry budget for lock contention.
//     Without this, SQLite returns SQLITE_BUSY immediately.
//   - foreign_keys = ON ......... FK constraints are off by default in SQLite.
//   - temp_store = MEMORY ....... Temp tables and indexes live in RAM.
//   - mmap_size = 268435456 ..... 256 MB mmap window for reads; reduces read
//     syscalls on hot pages.
var sqlitePragmas = []string{
	"journal_mode=WAL",
	"synchronous=NORMAL",
	"busy_timeout=5000",
	"foreign_keys=ON",
	"temp_store=MEMORY",
	"mmap_size=268435456",
}

// dbExecutor abstracts the query surface shared by *sql.DB and *sql.Tx so
// that Store methods compose inside Transact without duplication. See
// P2-ARCH-01 for the design rationale.
type dbExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store persists control-plane records in a local SQLite database.
//
// Store methods reference s.db via the dbExecutor interface so the same
// method bodies can run against a *sql.DB (outside Transact) or a
// *sql.Tx (inside Transact). s.sqlDB is the pool handle used for
// lifecycle (Ping, Close, BeginTx); it is nil on transaction-bound
// Stores to prevent accidental escape from the transaction boundary.
type Store struct {
	db    dbExecutor
	sqlDB *sql.DB
}

// Open opens a SQLite database file, applies the schema, and returns a storage
// backend.
//
// The DSN must be an on-disk file path. In-memory databases (":memory:") are
// rejected because WAL mode requires a real file — SQLite silently downgrades
// journal_mode to "memory" for in-memory databases, which defeats the
// concurrency guarantees the rest of the control-plane relies on. Tests that
// need a transient database should use `t.TempDir()` + a filename instead.
//
// Open uses context.Background() for migrations and the initial Ping; callers
// that need cancellation during startup should use OpenContext instead.
func Open(dsn string) (*Store, error) {
	return OpenContext(context.Background(), dsn)
}

// OpenContext is the context-aware variant of Open. It threads ctx through
// schema migration and the initial connectivity check so startup work can be
// cancelled by the caller.
func OpenContext(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == ":memory:" {
		return nil, fmt.Errorf("sqlite: in-memory DSN not supported; WAL requires an on-disk file")
	}

	if err := ensureParentDirectory(dsn); err != nil {
		return nil, err
	}

	dsnWithPragmas, err := appendPragmasToDSN(dsn, sqlitePragmas)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsnWithPragmas)
	if err != nil {
		return nil, err
	}

	// WAL permits concurrent readers while a single writer holds the log.
	// Previously MaxOpenConns was pinned to 1 because PRAGMA foreign_keys is
	// per-connection and we had no way to apply it to every pooled handle.
	// Pragmas are now applied via the `_pragma=` DSN parameter (see
	// modernc.org/sqlite conn.newConn -> applyQueryParams), so every
	// connection inherits them on Open. We can safely allow 4 connections:
	// one services writes, the rest serve concurrent reads. The 5s
	// busy_timeout absorbs transient lock contention without surfacing
	// SQLITE_BUSY to callers.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := MigrateContext(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db, sqlDB: db}, nil
}

// Transact runs fn inside a single SQLite transaction. BEGIN IMMEDIATE
// acquires the writer lock up front so the first write inside fn cannot
// fail with SQLITE_BUSY mid-transaction. On fn error or panic the
// transaction rolls back; on success it commits. SQLite is a single-
// writer engine, so there is no serialization-retry loop: contention
// is absorbed by busy_timeout at the connection level (see the pragmas
// above). See storage.Store.Transact for the full contract.
func (s *Store) Transact(ctx context.Context, fn storage.TxFn) (retErr error) {
	if s.sqlDB == nil {
		return storage.ErrNestedTransact
	}
	if fn == nil {
		return fmt.Errorf("sqlite: Transact requires a non-nil TxFn")
	}

	// BEGIN IMMEDIATE cannot be issued through BeginTx's options surface;
	// we issue it explicitly on a dedicated connection, run fn against a
	// tx-bound Store, then COMMIT / ROLLBACK on the same conn.
	conn, err := s.sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}

	committed := false
	// ROLLBACK runs in defer and must complete even when the caller's ctx
	// has already been canceled — otherwise we'd leave the writer lock
	// held. context.Background() is intentional here.
	defer func() { //nolint:contextcheck // deferred cleanup must outlive caller ctx
		if p := recover(); p != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
			panic(p)
		}
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	txStore := &Store{db: connExecutor{conn: conn}, sqlDB: nil}
	if err := fn(txStore); err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

// connExecutor adapts *sql.Conn to the dbExecutor interface. *sql.Conn
// already exposes ExecContext/QueryContext/QueryRowContext, but under
// different method set ownership rules; wrapping keeps tx-bound Stores
// honest: callers cannot reach through to BeginTx or Close.
type connExecutor struct {
	conn *sql.Conn
}

func (c connExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.conn.ExecContext(ctx, query, args...)
}

func (c connExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

func (c connExecutor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.conn.QueryRowContext(ctx, query, args...)
}

// txHandle abstracts the commit/rollback surface of *sql.Tx so that
// internal per-method transactions (e.g. ConsumeEnrollmentToken) can
// either open a fresh tx (top-level Store) or reuse the caller's tx
// (Store bound inside Transact) without changing method bodies.
type txHandle interface {
	dbExecutor
	Commit() error
	Rollback() error
}

// passthroughTx wraps an already-open transaction so that Commit /
// Rollback are no-ops: the outer Transact owns the transaction
// lifecycle and must not have it closed out from under it.
type passthroughTx struct {
	dbExecutor
}

func (p passthroughTx) Commit() error   { return nil }
func (p passthroughTx) Rollback() error { return nil }

// beginInternalTx returns a txHandle the caller can drive. When the
// Store is top-level it starts a new transaction; when the Store is
// already inside a Transact (sqlDB == nil) it returns a passthrough
// that reuses the current executor, so the caller's writes land in
// the outer transaction.
func (s *Store) beginInternalTx(ctx context.Context) (txHandle, error) {
	if s.sqlDB == nil {
		return passthroughTx{dbExecutor: s.db}, nil
	}
	return s.sqlDB.BeginTx(ctx, nil)
}

// appendPragmasToDSN rewrites the DSN so that every connection opened by the
// driver applies the given pragmas at startup. modernc.org/sqlite splits the
// DSN on the first '?' and parses everything after it as url.Values, then
// calls `PRAGMA <v>` for each `_pragma=<v>` value on every new connection.
func appendPragmasToDSN(dsn string, pragmas []string) (string, error) {
	path := dsn
	existing := url.Values{}

	if idx := strings.IndexRune(dsn, '?'); idx >= 0 {
		path = dsn[:idx]
		parsed, err := url.ParseQuery(dsn[idx+1:])
		if err != nil {
			return "", err
		}
		existing = parsed
	}

	for _, p := range pragmas {
		existing.Add("_pragma", p)
	}

	return path + "?" + existing.Encode(), nil
}

func ensureParentDirectory(dsn string) error {
	parent := filepath.Dir(dsn)
	if parent == "." || parent == "" {
		return nil
	}

	return os.MkdirAll(parent, 0o755)
}

// Ping verifies that the database connection is alive.
func (s *Store) Ping(ctx context.Context) error {
	if s.sqlDB == nil {
		return nil
	}
	return s.sqlDB.PingContext(ctx)
}

// Close releases the database handle owned by the store.
func (s *Store) Close() error {
	if s.sqlDB == nil {
		return nil
	}
	return s.sqlDB.Close()
}

func (s *Store) PutUser(ctx context.Context, user storage.UserRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, totp_enabled, totp_secret, created_at_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			password_hash = excluded.password_hash,
			role = excluded.role,
			totp_enabled = excluded.totp_enabled,
			totp_secret = excluded.totp_secret,
			created_at_unix = excluded.created_at_unix
	`, user.ID, user.Username, user.PasswordHash, user.Role, user.TotpEnabled, user.TotpSecret, toUnix(user.CreatedAt))
	return err
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (storage.UserRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at_unix
		FROM users
		WHERE id = ?
	`, userID)

	var user storage.UserRecord
	var createdAt int64
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.UserRecord{}, storage.ErrNotFound
		}
		return storage.UserRecord{}, err
	}

	user.CreatedAt = fromUnix(createdAt)
	return user, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (storage.UserRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at_unix
		FROM users
		WHERE username = ?
	`, username)

	var user storage.UserRecord
	var createdAt int64
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.UserRecord{}, storage.ErrNotFound
		}
		return storage.UserRecord{}, err
	}

	user.CreatedAt = fromUnix(createdAt)
	return user, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]storage.UserRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at_unix
		FROM users
		ORDER BY created_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.UserRecord, 0)
	for rows.Next() {
		var user storage.UserRecord
		var createdAt int64
		if err := rows.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &createdAt); err != nil {
			return nil, err
		}
		user.CreatedAt = fromUnix(createdAt)
		result = append(result, user)
	}

	return result, rows.Err()
}

func (s *Store) PutFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	updatedAt := group.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = group.CreatedAt
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_groups (id, name, label, description, created_at_unix, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name            = excluded.name,
			label           = excluded.label,
			description     = excluded.description,
			created_at_unix = excluded.created_at_unix,
			updated_at_unix = excluded.updated_at_unix
	`, group.ID, group.Name, group.Label, group.Description, toUnix(group.CreatedAt), toUnix(updatedAt))
	return err
}

func (s *Store) CreateFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_groups (id, name, label, description, created_at_unix, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
	`, group.ID, group.Name, group.Label, group.Description, toUnix(group.CreatedAt), toUnix(group.UpdatedAt))
	return err
}

// UpdateFleetGroup modifies editable fields only. `name` is the
// immutable slug and is intentionally absent from the SET list.
func (s *Store) UpdateFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE fleet_groups
		SET label           = ?,
		    description     = ?,
		    updated_at_unix = ?
		WHERE id = ?
	`, group.Label, group.Description, toUnix(group.UpdatedAt), group.ID)
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

func (s *Store) GetFleetGroup(ctx context.Context, id string) (storage.FleetGroupRecord, error) {
	return s.scanFleetGroupRow(ctx, `
		SELECT id, name, label, description, created_at_unix, updated_at_unix
		FROM fleet_groups
		WHERE id = ?
	`, id)
}

func (s *Store) GetFleetGroupByName(ctx context.Context, name string) (storage.FleetGroupRecord, error) {
	return s.scanFleetGroupRow(ctx, `
		SELECT id, name, label, description, created_at_unix, updated_at_unix
		FROM fleet_groups
		WHERE name = ?
	`, name)
}

func (s *Store) scanFleetGroupRow(ctx context.Context, query string, arg string) (storage.FleetGroupRecord, error) {
	var group storage.FleetGroupRecord
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, query, arg).Scan(
		&group.ID, &group.Name, &group.Label, &group.Description, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.FleetGroupRecord{}, storage.ErrNotFound
		}
		return storage.FleetGroupRecord{}, err
	}
	group.CreatedAt = fromUnix(createdAt)
	group.UpdatedAt = fromUnix(updatedAt)
	return group, nil
}

func (s *Store) ListFleetGroups(ctx context.Context) ([]storage.FleetGroupRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, label, description, created_at_unix, updated_at_unix
		FROM fleet_groups
		ORDER BY created_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.FleetGroupRecord, 0)
	for rows.Next() {
		var group storage.FleetGroupRecord
		var createdAt, updatedAt int64
		if err := rows.Scan(
			&group.ID, &group.Name, &group.Label, &group.Description, &createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		group.CreatedAt = fromUnix(createdAt)
		group.UpdatedAt = fromUnix(updatedAt)
		result = append(result, group)
	}

	return result, rows.Err()
}

func (s *Store) DeleteFleetGroup(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM fleet_groups WHERE id = ?`, id)
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

func (s *Store) CountFleetGroupMembers(ctx context.Context, fleetGroupID string) (storage.ReassignCounts, error) {
	var counts storage.ReassignCounts
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM agents              WHERE fleet_group_id = ?),
			(SELECT COUNT(*) FROM enrollment_tokens   WHERE fleet_group_id = ?),
			(SELECT COUNT(*) FROM client_assignments  WHERE fleet_group_id = ?)
	`, fleetGroupID, fleetGroupID, fleetGroupID).Scan(
		&counts.Agents, &counts.EnrollmentTokens, &counts.ClientAssignments,
	)
	if err != nil {
		return storage.ReassignCounts{}, err
	}
	return counts, nil
}

// ReassignFleetGroupMembers is NOT atomic on its own — callers must
// wrap the full delete flow in Store.Transact so partial progress is
// not visible on crash. See fleet.Service.Delete.
func (s *Store) ReassignFleetGroupMembers(ctx context.Context, fromID, toID string) (storage.ReassignCounts, error) {
	var counts storage.ReassignCounts
	updates := []struct {
		stmt  string
		field *int64
	}{
		{`UPDATE agents             SET fleet_group_id = ? WHERE fleet_group_id = ?`, &counts.Agents},
		{`UPDATE enrollment_tokens  SET fleet_group_id = ? WHERE fleet_group_id = ?`, &counts.EnrollmentTokens},
		{`UPDATE client_assignments SET fleet_group_id = ? WHERE fleet_group_id = ?`, &counts.ClientAssignments},
	}
	for _, u := range updates {
		result, err := s.db.ExecContext(ctx, u.stmt, toID, fromID)
		if err != nil {
			return storage.ReassignCounts{}, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return storage.ReassignCounts{}, err
		}
		*u.field = n
	}
	return counts, nil
}

// CreateIntegrationProvider inserts a new provider row. Config is
// opaque JSON bytes — the caller is responsible for kind-specific
// validation before writing.
func (s *Store) CreateIntegrationProvider(ctx context.Context, provider storage.IntegrationProviderRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO integration_providers (id, kind, label, config, created_at_unix, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
	`, provider.ID, provider.Kind, provider.Label, string(provider.Config),
		toUnix(provider.CreatedAt), toUnix(provider.UpdatedAt))
	return err
}

func (s *Store) UpdateIntegrationProvider(ctx context.Context, provider storage.IntegrationProviderRecord) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE integration_providers
		SET label           = ?,
		    config          = ?,
		    updated_at_unix = ?
		WHERE id = ?
	`, provider.Label, string(provider.Config), toUnix(provider.UpdatedAt), provider.ID)
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

func (s *Store) GetIntegrationProvider(ctx context.Context, id string) (storage.IntegrationProviderRecord, error) {
	var p storage.IntegrationProviderRecord
	var config string
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, label, config, created_at_unix, updated_at_unix
		FROM integration_providers
		WHERE id = ?
	`, id).Scan(&p.ID, &p.Kind, &p.Label, &config, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.IntegrationProviderRecord{}, storage.ErrNotFound
		}
		return storage.IntegrationProviderRecord{}, err
	}
	p.Config = []byte(config)
	p.CreatedAt = fromUnix(createdAt)
	p.UpdatedAt = fromUnix(updatedAt)
	return p, nil
}

func (s *Store) ListIntegrationProviders(ctx context.Context) ([]storage.IntegrationProviderRecord, error) {
	return s.scanIntegrationProviders(ctx, `
		SELECT id, kind, label, config, created_at_unix, updated_at_unix
		FROM integration_providers
		ORDER BY kind, created_at_unix, id
	`)
}

func (s *Store) ListIntegrationProvidersByKind(ctx context.Context, kind string) ([]storage.IntegrationProviderRecord, error) {
	return s.scanIntegrationProviders(ctx, `
		SELECT id, kind, label, config, created_at_unix, updated_at_unix
		FROM integration_providers
		WHERE kind = ?
		ORDER BY created_at_unix, id
	`, kind)
}

func (s *Store) scanIntegrationProviders(ctx context.Context, query string, args ...any) ([]storage.IntegrationProviderRecord, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.IntegrationProviderRecord, 0)
	for rows.Next() {
		var p storage.IntegrationProviderRecord
		var config string
		var createdAt, updatedAt int64
		if err := rows.Scan(&p.ID, &p.Kind, &p.Label, &config, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.Config = []byte(config)
		p.CreatedAt = fromUnix(createdAt)
		p.UpdatedAt = fromUnix(updatedAt)
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) DeleteIntegrationProvider(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM integration_providers WHERE id = ?`, id)
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

func (s *Store) CreateFleetGroupIntegration(ctx context.Context, i storage.FleetGroupIntegrationRecord) error {
	providerID := sql.NullString{}
	if i.ProviderID != nil {
		providerID.Valid = true
		providerID.String = *i.ProviderID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_group_integrations
			(id, fleet_group_id, kind, provider_id, config, enabled, created_at_unix, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, i.ID, i.FleetGroupID, i.Kind, providerID, string(i.Config),
		boolToInt(i.Enabled), toUnix(i.CreatedAt), toUnix(i.UpdatedAt))
	return err
}

func (s *Store) UpdateFleetGroupIntegration(ctx context.Context, i storage.FleetGroupIntegrationRecord) error {
	providerID := sql.NullString{}
	if i.ProviderID != nil {
		providerID.Valid = true
		providerID.String = *i.ProviderID
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE fleet_group_integrations
		SET provider_id     = ?,
		    config          = ?,
		    enabled         = ?,
		    updated_at_unix = ?
		WHERE id = ?
	`, providerID, string(i.Config), boolToInt(i.Enabled), toUnix(i.UpdatedAt), i.ID)
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

func (s *Store) GetFleetGroupIntegration(ctx context.Context, id string) (storage.FleetGroupIntegrationRecord, error) {
	var i storage.FleetGroupIntegrationRecord
	var providerID sql.NullString
	var config string
	var enabled int
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, fleet_group_id, kind, provider_id, config, enabled, created_at_unix, updated_at_unix
		FROM fleet_group_integrations
		WHERE id = ?
	`, id).Scan(&i.ID, &i.FleetGroupID, &i.Kind, &providerID, &config, &enabled, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.FleetGroupIntegrationRecord{}, storage.ErrNotFound
		}
		return storage.FleetGroupIntegrationRecord{}, err
	}
	if providerID.Valid {
		pid := providerID.String
		i.ProviderID = &pid
	}
	i.Config = []byte(config)
	i.Enabled = enabled != 0
	i.CreatedAt = fromUnix(createdAt)
	i.UpdatedAt = fromUnix(updatedAt)
	return i, nil
}

func (s *Store) ListFleetGroupIntegrations(ctx context.Context, fleetGroupID string) ([]storage.FleetGroupIntegrationRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, fleet_group_id, kind, provider_id, config, enabled, created_at_unix, updated_at_unix
		FROM fleet_group_integrations
		WHERE fleet_group_id = ?
		ORDER BY kind, created_at_unix, id
	`, fleetGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.FleetGroupIntegrationRecord, 0)
	for rows.Next() {
		var i storage.FleetGroupIntegrationRecord
		var providerID sql.NullString
		var config string
		var enabled int
		var createdAt, updatedAt int64
		if err := rows.Scan(&i.ID, &i.FleetGroupID, &i.Kind, &providerID, &config, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if providerID.Valid {
			pid := providerID.String
			i.ProviderID = &pid
		}
		i.Config = []byte(config)
		i.Enabled = enabled != 0
		i.CreatedAt = fromUnix(createdAt)
		i.UpdatedAt = fromUnix(updatedAt)
		result = append(result, i)
	}
	return result, rows.Err()
}

func (s *Store) DeleteFleetGroupIntegration(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM fleet_group_integrations WHERE id = ?`, id)
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

func (s *Store) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_name, fleet_group_id, version, read_only, last_seen_at_unix, cert_issued_at_unix, cert_expires_at_unix
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
		if err := rows.Scan(&agent.ID, &agent.NodeName, &fleetGroupID, &agent.Version, &readOnly, &lastSeenAt, &certIssuedAtUnix, &certExpiresAtUnix); err != nil {
			return nil, err
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

func (s *Store) PutInstance(ctx context.Context, instance storage.InstanceRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO telemt_instances (id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			agent_id = excluded.agent_id,
			name = excluded.name,
			version = excluded.version,
			config_fingerprint = excluded.config_fingerprint,
			connected_users = excluded.connected_users,
			read_only = excluded.read_only,
			updated_at_unix = excluded.updated_at_unix
	`, instance.ID, instance.AgentID, instance.Name, instance.Version, instance.ConfigFingerprint, instance.ConnectedUsers, boolToInt(instance.ReadOnly), toUnix(instance.UpdatedAt))
	return err
}

func (s *Store) ListInstances(ctx context.Context) ([]storage.InstanceRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at_unix
		FROM telemt_instances
		ORDER BY updated_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.InstanceRecord, 0)
	for rows.Next() {
		var instance storage.InstanceRecord
		var readOnly int
		var updatedAt int64
		if err := rows.Scan(&instance.ID, &instance.AgentID, &instance.Name, &instance.Version, &instance.ConfigFingerprint, &instance.ConnectedUsers, &readOnly, &updatedAt); err != nil {
			return nil, err
		}
		instance.ReadOnly = intToBool(readOnly)
		instance.UpdatedAt = fromUnix(updatedAt)
		result = append(result, instance)
	}

	return result, rows.Err()
}

func (s *Store) PutJob(ctx context.Context, job storage.JobRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			action = excluded.action,
			actor_id = excluded.actor_id,
			status = excluded.status,
			created_at_unix = excluded.created_at_unix,
			ttl_nanos = excluded.ttl_nanos,
			idempotency_key = excluded.idempotency_key,
			payload_json = excluded.payload_json
	`, job.ID, job.Action, job.ActorID, job.Status, toUnix(job.CreatedAt), job.TTL.Nanoseconds(), job.IdempotencyKey, job.PayloadJSON)
	return err
}

func (s *Store) GetJobByIdempotencyKey(ctx context.Context, idempotencyKey string) (storage.JobRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json
		FROM jobs
		WHERE idempotency_key = ?
	`, idempotencyKey)

	var job storage.JobRecord
	var createdAt int64
	var ttlNanos int64
	if err := row.Scan(&job.ID, &job.Action, &job.ActorID, &job.Status, &createdAt, &ttlNanos, &job.IdempotencyKey, &job.PayloadJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.JobRecord{}, storage.ErrNotFound
		}
		return storage.JobRecord{}, err
	}

	job.CreatedAt = fromUnix(createdAt)
	job.TTL = time.Duration(ttlNanos)
	return job, nil
}

func (s *Store) ListJobs(ctx context.Context) ([]storage.JobRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json
		FROM jobs
		ORDER BY created_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.JobRecord, 0)
	for rows.Next() {
		var job storage.JobRecord
		var createdAt int64
		var ttlNanos int64
		if err := rows.Scan(&job.ID, &job.Action, &job.ActorID, &job.Status, &createdAt, &ttlNanos, &job.IdempotencyKey, &job.PayloadJSON); err != nil {
			return nil, err
		}
		job.CreatedAt = fromUnix(createdAt)
		job.TTL = time.Duration(ttlNanos)
		result = append(result, job)
	}

	return result, rows.Err()
}

func (s *Store) PutJobTarget(ctx context.Context, target storage.JobTargetRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO job_targets (job_id, agent_id, status, result_text, result_json, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id, agent_id) DO UPDATE SET
			status = excluded.status,
			result_text = excluded.result_text,
			result_json = excluded.result_json,
			updated_at_unix = excluded.updated_at_unix
	`, target.JobID, target.AgentID, target.Status, target.ResultText, target.ResultJSON, toUnix(target.UpdatedAt))
	return err
}

func (s *Store) ListJobTargets(ctx context.Context, jobID string) ([]storage.JobTargetRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id, agent_id, status, result_text, result_json, updated_at_unix
		FROM job_targets
		WHERE job_id = ?
		ORDER BY agent_id
	`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.JobTargetRecord, 0)
	for rows.Next() {
		var target storage.JobTargetRecord
		var updatedAt int64
		if err := rows.Scan(&target.JobID, &target.AgentID, &target.Status, &target.ResultText, &target.ResultJSON, &updatedAt); err != nil {
			return nil, err
		}
		target.UpdatedAt = fromUnix(updatedAt)
		result = append(result, target)
	}

	return result, rows.Err()
}

func (s *Store) AppendAuditEvent(ctx context.Context, event storage.AuditEventRecord) error {
	detailsJSON, err := encodeJSON(event.Details)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO audit_events (id, actor_id, action, target_id, created_at_unix, details)
		VALUES (?, ?, ?, ?, ?, ?)
	`, event.ID, event.ActorID, event.Action, event.TargetID, toUnix(event.CreatedAt), detailsJSON)
	return err
}

func (s *Store) ListAuditEvents(ctx context.Context, limit int) ([]storage.AuditEventRecord, error) {
	if limit <= 0 {
		limit = 1024
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_id, action, target_id, created_at_unix, details
		FROM (SELECT * FROM audit_events ORDER BY created_at_unix DESC, id DESC LIMIT ?)
		ORDER BY created_at_unix, id
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AuditEventRecord, 0)
	for rows.Next() {
		var event storage.AuditEventRecord
		var createdAt int64
		var detailsJSON string
		if err := rows.Scan(&event.ID, &event.ActorID, &event.Action, &event.TargetID, &createdAt, &detailsJSON); err != nil {
			return nil, err
		}
		event.CreatedAt = fromUnix(createdAt)
		if err := decodeJSON(detailsJSON, &event.Details); err != nil {
			return nil, err
		}
		result = append(result, event)
	}

	return result, rows.Err()
}

// PruneAuditEvents deletes audit_events rows strictly older than before and
// returns the RowsAffected count. Exec-based to avoid pulling all rows through
// Go for retention worker efficiency (P2-REL-04).
func (s *Store) PruneAuditEvents(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM audit_events WHERE created_at_unix < ?`, toUnix(before))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) AppendMetricSnapshot(ctx context.Context, snapshot storage.MetricSnapshotRecord) error {
	valuesJSON, err := encodeJSON(snapshot.Values)
	if err != nil {
		return err
	}

	// `values` is a reserved keyword in SQLite, so the identifier must be
	// double-quoted. The column was renamed from `values_json` in migration
	// 0011 (P2-DB-05 / DF-25) to match the Postgres schema.
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO metric_snapshots (id, agent_id, instance_id, captured_at_unix, "values")
		VALUES (?, ?, ?, ?, ?)
	`, snapshot.ID, snapshot.AgentID, snapshot.InstanceID, toUnix(snapshot.CapturedAt), valuesJSON)
	return err
}

func (s *Store) ListMetricSnapshots(ctx context.Context) ([]storage.MetricSnapshotRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, instance_id, captured_at_unix, "values"
		FROM (SELECT * FROM metric_snapshots ORDER BY captured_at_unix DESC, id DESC LIMIT 512)
		ORDER BY captured_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.MetricSnapshotRecord, 0)
	for rows.Next() {
		var snapshot storage.MetricSnapshotRecord
		var capturedAt int64
		var valuesJSON string
		if err := rows.Scan(&snapshot.ID, &snapshot.AgentID, &snapshot.InstanceID, &capturedAt, &valuesJSON); err != nil {
			return nil, err
		}
		snapshot.CapturedAt = fromUnix(capturedAt)
		if err := decodeJSON(valuesJSON, &snapshot.Values); err != nil {
			return nil, err
		}
		result = append(result, snapshot)
	}

	return result, rows.Err()
}

// PruneMetricSnapshots deletes metric_snapshots rows strictly older than
// before and returns the RowsAffected count (P2-REL-05).
func (s *Store) PruneMetricSnapshots(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM metric_snapshots WHERE captured_at_unix < ?`, toUnix(before))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) PutEnrollmentToken(ctx context.Context, token storage.EnrollmentTokenRecord) error {
	var fleetGroupID sql.NullString
	if token.FleetGroupID != "" {
		fleetGroupID.Valid = true
		fleetGroupID.String = token.FleetGroupID
	}
	var consumedAt sql.NullInt64
	if token.ConsumedAt != nil {
		consumedAt.Valid = true
		consumedAt.Int64 = toUnix(*token.ConsumedAt)
	}
	var revokedAt sql.NullInt64
	if token.RevokedAt != nil {
		revokedAt.Valid = true
		revokedAt.Int64 = toUnix(*token.RevokedAt)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO enrollment_tokens (value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(value) DO UPDATE SET
			fleet_group_id = excluded.fleet_group_id,
			issued_at_unix = excluded.issued_at_unix,
			expires_at_unix = excluded.expires_at_unix,
			consumed_at_unix = excluded.consumed_at_unix,
			revoked_at_unix = excluded.revoked_at_unix
	`, token.Value, fleetGroupID, toUnix(token.IssuedAt), toUnix(token.ExpiresAt), consumedAt, revokedAt)
	return err
}

func (s *Store) ListEnrollmentTokens(ctx context.Context) ([]storage.EnrollmentTokenRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
		FROM enrollment_tokens
		ORDER BY issued_at_unix, value
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.EnrollmentTokenRecord, 0)
	for rows.Next() {
		var token storage.EnrollmentTokenRecord
		var fleetGroupID sql.NullString
		var issuedAt int64
		var expiresAt int64
		var consumedAt sql.NullInt64
		var revokedAt sql.NullInt64
		if err := rows.Scan(&token.Value, &fleetGroupID, &issuedAt, &expiresAt, &consumedAt, &revokedAt); err != nil {
			return nil, err
		}
		if fleetGroupID.Valid {
			token.FleetGroupID = fleetGroupID.String
		}
		token.IssuedAt = fromUnix(issuedAt)
		token.ExpiresAt = fromUnix(expiresAt)
		if consumedAt.Valid {
			timeValue := fromUnix(consumedAt.Int64)
			token.ConsumedAt = &timeValue
		}
		if revokedAt.Valid {
			timeValue := fromUnix(revokedAt.Int64)
			token.RevokedAt = &timeValue
		}
		result = append(result, token)
	}

	return result, rows.Err()
}

func (s *Store) GetEnrollmentToken(ctx context.Context, value string) (storage.EnrollmentTokenRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
		FROM enrollment_tokens
		WHERE value = ?
	`, value)

	var token storage.EnrollmentTokenRecord
	var fleetGroupID sql.NullString
	var issuedAt int64
	var expiresAt int64
	var consumedAt sql.NullInt64
	var revokedAt sql.NullInt64
	if err := row.Scan(&token.Value, &fleetGroupID, &issuedAt, &expiresAt, &consumedAt, &revokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
		}
		return storage.EnrollmentTokenRecord{}, err
	}

	if fleetGroupID.Valid {
		token.FleetGroupID = fleetGroupID.String
	}
	token.IssuedAt = fromUnix(issuedAt)
	token.ExpiresAt = fromUnix(expiresAt)
	if consumedAt.Valid {
		timeValue := fromUnix(consumedAt.Int64)
		token.ConsumedAt = &timeValue
	}
	if revokedAt.Valid {
		timeValue := fromUnix(revokedAt.Int64)
		token.RevokedAt = &timeValue
	}

	return token, nil
}

func (s *Store) ConsumeEnrollmentToken(ctx context.Context, value string, consumedAt time.Time) (storage.EnrollmentTokenRecord, error) {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
		FROM enrollment_tokens
		WHERE value = ?
	`, value)

	var token storage.EnrollmentTokenRecord
	var fleetGroupID sql.NullString
	var issuedAt int64
	var expiresAt int64
	var storedConsumedAt sql.NullInt64
	var storedRevokedAt sql.NullInt64
	if err := row.Scan(&token.Value, &fleetGroupID, &issuedAt, &expiresAt, &storedConsumedAt, &storedRevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
		}
		return storage.EnrollmentTokenRecord{}, err
	}

	if storedConsumedAt.Valid || storedRevokedAt.Valid {
		return storage.EnrollmentTokenRecord{}, storage.ErrConflict
	}

	if fleetGroupID.Valid {
		token.FleetGroupID = fleetGroupID.String
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE enrollment_tokens
		SET consumed_at_unix = ?
		WHERE value = ? AND consumed_at_unix IS NULL AND revoked_at_unix IS NULL
	`, toUnix(consumedAt), value)
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	if rowsAffected == 0 {
		return storage.EnrollmentTokenRecord{}, storage.ErrConflict
	}

	if err := tx.Commit(); err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}

	token.IssuedAt = fromUnix(issuedAt)
	token.ExpiresAt = fromUnix(expiresAt)
	token.ConsumedAt = &consumedAt
	return token, nil
}

func (s *Store) RevokeEnrollmentToken(ctx context.Context, value string, revokedAt time.Time) (storage.EnrollmentTokenRecord, error) {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
		FROM enrollment_tokens
		WHERE value = ?
	`, value)

	var token storage.EnrollmentTokenRecord
	var fleetGroupID sql.NullString
	var issuedAt int64
	var expiresAt int64
	var storedConsumedAt sql.NullInt64
	var storedRevokedAt sql.NullInt64
	if err := row.Scan(&token.Value, &fleetGroupID, &issuedAt, &expiresAt, &storedConsumedAt, &storedRevokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.EnrollmentTokenRecord{}, storage.ErrNotFound
		}
		return storage.EnrollmentTokenRecord{}, err
	}

	if fleetGroupID.Valid {
		token.FleetGroupID = fleetGroupID.String
	}
	token.IssuedAt = fromUnix(issuedAt)
	token.ExpiresAt = fromUnix(expiresAt)
	if storedConsumedAt.Valid {
		timeValue := fromUnix(storedConsumedAt.Int64)
		token.ConsumedAt = &timeValue
	}
	if storedRevokedAt.Valid {
		timeValue := fromUnix(storedRevokedAt.Int64)
		token.RevokedAt = &timeValue
		return token, nil
	}
	if storedConsumedAt.Valid {
		return token, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE enrollment_tokens
		SET revoked_at_unix = ?
		WHERE value = ? AND consumed_at_unix IS NULL AND revoked_at_unix IS NULL
	`, toUnix(revokedAt), value)
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}
	if rowsAffected == 0 {
		return storage.EnrollmentTokenRecord{}, storage.ErrConflict
	}

	if err := tx.Commit(); err != nil {
		return storage.EnrollmentTokenRecord{}, err
	}

	revokedValue := revokedAt.UTC()
	token.RevokedAt = &revokedValue
	return token, nil
}

func (s *Store) PutAgentCertificateRecoveryGrant(ctx context.Context, grant storage.AgentCertificateRecoveryGrantRecord) error {
	var usedAt any
	if grant.UsedAt != nil {
		usedAt = toUnix(*grant.UsedAt)
	}
	var revokedAt any
	if grant.RevokedAt != nil {
		revokedAt = toUnix(*grant.RevokedAt)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_certificate_recovery_grants (agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			issued_by = excluded.issued_by,
			issued_at_unix = excluded.issued_at_unix,
			expires_at_unix = excluded.expires_at_unix,
			used_at_unix = excluded.used_at_unix,
			revoked_at_unix = excluded.revoked_at_unix
	`, grant.AgentID, grant.IssuedBy, toUnix(grant.IssuedAt), toUnix(grant.ExpiresAt), usedAt, revokedAt)
	return err
}

func (s *Store) ListAgentCertificateRecoveryGrants(ctx context.Context) ([]storage.AgentCertificateRecoveryGrantRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix
		FROM agent_certificate_recovery_grants
		ORDER BY issued_at_unix, agent_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AgentCertificateRecoveryGrantRecord, 0)
	for rows.Next() {
		grant, err := scanAgentCertificateRecoveryGrantRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, grant)
	}

	return result, rows.Err()
}

func (s *Store) GetAgentCertificateRecoveryGrant(ctx context.Context, agentID string) (storage.AgentCertificateRecoveryGrantRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix
		FROM agent_certificate_recovery_grants
		WHERE agent_id = ?
	`, agentID)

	grant, err := scanAgentCertificateRecoveryGrantRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
		}
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}

	return grant, nil
}

func (s *Store) UseAgentCertificateRecoveryGrant(ctx context.Context, agentID string, usedAt time.Time) (storage.AgentCertificateRecoveryGrantRecord, error) {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix
		FROM agent_certificate_recovery_grants
		WHERE agent_id = ?
	`, agentID)

	grant, err := scanAgentCertificateRecoveryGrantRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
		}
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	if grant.UsedAt != nil || grant.RevokedAt != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrConflict
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE agent_certificate_recovery_grants
		SET used_at_unix = ?
		WHERE agent_id = ? AND used_at_unix IS NULL AND revoked_at_unix IS NULL
	`, toUnix(usedAt), agentID)
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	if rowsAffected == 0 {
		return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}

	usedValue := usedAt.UTC()
	grant.UsedAt = &usedValue
	return grant, nil
}

func (s *Store) RevokeAgentCertificateRecoveryGrant(ctx context.Context, agentID string, revokedAt time.Time) (storage.AgentCertificateRecoveryGrantRecord, error) {
	tx, err := s.beginInternalTx(ctx)
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		SELECT agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix
		FROM agent_certificate_recovery_grants
		WHERE agent_id = ?
	`, agentID)

	grant, err := scanAgentCertificateRecoveryGrantRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrNotFound
		}
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	if grant.RevokedAt != nil || grant.UsedAt != nil {
		return grant, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE agent_certificate_recovery_grants
		SET revoked_at_unix = ?
		WHERE agent_id = ? AND used_at_unix IS NULL AND revoked_at_unix IS NULL
	`, toUnix(revokedAt), agentID)
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}
	if rowsAffected == 0 {
		return storage.AgentCertificateRecoveryGrantRecord{}, storage.ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}

	revokedValue := revokedAt.UTC()
	grant.RevokedAt = &revokedValue
	return grant, nil
}

type agentCertificateRecoveryGrantScanner interface {
	Scan(dest ...any) error
}

func scanAgentCertificateRecoveryGrantRow(scanner agentCertificateRecoveryGrantScanner) (storage.AgentCertificateRecoveryGrantRecord, error) {
	var grant storage.AgentCertificateRecoveryGrantRecord
	var issuedAt int64
	var expiresAt int64
	var usedAt sql.NullInt64
	var revokedAt sql.NullInt64
	if err := scanner.Scan(&grant.AgentID, &grant.IssuedBy, &issuedAt, &expiresAt, &usedAt, &revokedAt); err != nil {
		return storage.AgentCertificateRecoveryGrantRecord{}, err
	}

	grant.IssuedAt = fromUnix(issuedAt)
	grant.ExpiresAt = fromUnix(expiresAt)
	if usedAt.Valid {
		timeValue := fromUnix(usedAt.Int64)
		grant.UsedAt = &timeValue
	}
	if revokedAt.Valid {
		timeValue := fromUnix(revokedAt.Int64)
		grant.RevokedAt = &timeValue
	}

	return grant, nil
}

func toUnix(value time.Time) int64 {
	return value.UTC().Unix()
}

func fromUnix(value int64) time.Time {
	return time.Unix(value, 0).UTC()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func intToBool(value int) bool {
	return value != 0
}

func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func decodeJSON[T any](value string, target *T) error {
	if value == "" {
		value = "{}"
	}

	return json.Unmarshal([]byte(value), target)
}
