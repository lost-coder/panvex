package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Retention is persisted as an opaque JSON blob in the
// panel_settings.retention_json column, keyed by scope='panel'. This
// piggy-backs on the existing singleton row: a single migration
// (0009) added the column, and subsequent retention-knob additions
// need no further migrations — the JSON schema evolves freely.
//
// An empty (or missing) retention_json column is treated as
// ErrNotFound so the caller (server.New) falls back to defaults.

func (s *Store) GetRetentionSettings(ctx context.Context) (storage.RetentionSettings, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `
		SELECT retention_json
		FROM panel_settings
		WHERE scope = $1
	`, panelSettingsScope).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.RetentionSettings{}, storage.ErrNotFound
		}
		return storage.RetentionSettings{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return storage.RetentionSettings{}, storage.ErrNotFound
	}

	var settings storage.RetentionSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return storage.RetentionSettings{}, err
	}
	return settings, nil
}

func (s *Store) PutRetentionSettings(ctx context.Context, settings storage.RetentionSettings) error {
	payload, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	// UPSERT: if the panel_settings row does not yet exist (operator has
	// never opened the panel settings page), create it with empty panel
	// fields and the retention blob. If it does exist, update only the
	// retention_json column and bump updated_at.
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO panel_settings (
			scope,
			http_public_url,
			grpc_public_endpoint,
			retention_json,
			updated_at
		)
		VALUES ($1, '', '', $2, $3)
		ON CONFLICT (scope) DO UPDATE
		SET retention_json = EXCLUDED.retention_json,
		    updated_at = EXCLUDED.updated_at
	`, panelSettingsScope, string(payload), time.Now().UTC())
	return err
}
