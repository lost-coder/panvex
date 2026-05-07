package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// DBStore implements StoreReader and StoreWriter against a raw *sql.DB
// plus sqlc-generated queries. It scopes panel_settings to the canonical
// "default" scope.
//
// Placeholder style: uses ? (SQLite). Postgres callers would need a
// separate adapter; DBStore is currently wired only from the sqlite path.
type DBStore struct {
	db *sql.DB
	q  *dbsqlc.Queries
}

// NewDBStore wraps a *sql.DB. dbsqlc.New(db) is called internally so the
// same instance can serve as both StoreReader and StoreWriter passed to
// settings.NewOperationalStoreRW.
func NewDBStore(db *sql.DB) *DBStore {
	return &DBStore{db: db, q: dbsqlc.New(db)}
}

const settingsScope = "default"

// allowedPanelColumns matches the columns on panel_settings after the
// bootstrap-column drop migration (0036). Any column not in this set is
// rejected to prevent arbitrary mutation via a malformed registry tag.
var allowedPanelColumns = map[string]struct{}{
	"http_public_url":      {},
	"grpc_public_endpoint": {},
	"password_min_length":  {},
	"retention_json":       {},
	"geoip_json":           {},
	"geoip_state_json":     {},
}

// ReadPanelColumn reads a single named column from the panel_settings row
// for the canonical "default" scope. Returns "" (with nil error) when the
// column is NULL or the row doesn't exist yet.
func (s *DBStore) ReadPanelColumn(ctx context.Context, col string) (string, error) {
	if _, ok := allowedPanelColumns[col]; !ok {
		return "", fmt.Errorf("settings: column %q not on panel_settings", col)
	}
	//nolint:gosec // col validated against allowlist above
	q := fmt.Sprintf("SELECT %s FROM panel_settings WHERE scope = ?", col)
	row := s.db.QueryRowContext(ctx, q, settingsScope)
	var raw sql.NullString
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if !raw.Valid {
		return "", nil
	}
	return raw.String, nil
}

// WritePanelColumn upserts the panel_settings row and sets the named column.
// The who argument is accepted for interface compatibility but not persisted
// (panel_settings has no updated_by column).
func (s *DBStore) WritePanelColumn(ctx context.Context, col, raw, _ string) error {
	if _, ok := allowedPanelColumns[col]; !ok {
		return fmt.Errorf("settings: column %q not writable", col)
	}
	now := time.Now().Unix()
	// Ensure the row exists first.
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO panel_settings (scope, updated_at_unix) VALUES (?, ?)
		 ON CONFLICT (scope) DO NOTHING`,
		settingsScope, now); err != nil {
		return err
	}
	//nolint:gosec // col validated against allowlist above
	q := fmt.Sprintf("UPDATE panel_settings SET %s = ?, updated_at_unix = ? WHERE scope = ?", col)
	_, err := s.db.ExecContext(ctx, q, raw, now, settingsScope)
	return err
}

// ReadRuntimeSetting fetches a runtime setting by name. Returns
// sql.ErrNoRows (wrapped) when the name is not present.
func (s *DBStore) ReadRuntimeSetting(ctx context.Context, name string) (string, time.Time, string, error) {
	row, err := s.q.GetRuntimeSetting(ctx, name)
	if err != nil {
		return "", time.Time{}, "", err
	}
	return row.ValueJson, time.Unix(row.UpdatedAt, 0), row.UpdatedBy, nil
}

// WriteRuntimeSetting upserts a runtime setting.
func (s *DBStore) WriteRuntimeSetting(ctx context.Context, name, valueJSON, who string) error {
	return s.q.UpsertRuntimeSetting(ctx, dbsqlc.UpsertRuntimeSettingParams{
		Name:      name,
		ValueJson: valueJSON,
		UpdatedAt: time.Now().Unix(),
		UpdatedBy: who,
	})
}
