package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// Retention is persisted as an opaque JSON blob in the
// panel_settings.retention_json column, keyed by scope='panel'. This
// piggy-backs on the existing singleton row: a single migration
// (0009) added the column, and subsequent retention-knob additions
// need no further migrations — the JSON schema evolves freely.
//
// An empty (or missing) retention_json column is treated as
// ErrNotFound so the caller (server.New) falls back to defaults.
//
// R-Q-03: routed through dbsqlc.

func (s *Store) GetRetentionSettings(ctx context.Context) (storage.RetentionSettings, error) {
	raw, err := dbsqlc.New(s.db).GetRetentionJSON(ctx, panelSettingsScope)
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
	return dbsqlc.New(s.db).UpsertRetentionJSON(ctx, dbsqlc.UpsertRetentionJSONParams{
		Scope:         panelSettingsScope,
		RetentionJson: string(payload),
		UpdatedAt:     time.Now().UTC(),
	})
}
