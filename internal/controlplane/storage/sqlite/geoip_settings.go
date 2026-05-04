package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// GeoIP settings + state ride alongside the panel_settings singleton
// row in the geoip_json (operator-managed) and geoip_state_json
// (worker-managed) columns added by migration 0034. The storage layer
// treats both as opaque JSON blobs — schema validation is the server's
// job. Mirrors the retention_settings pattern: an empty/missing column
// returns (nil, nil) so callers can fall back to defaults.

func (s *Store) PutGeoIPSettings(ctx context.Context, data json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO panel_settings (
			scope,
			http_public_url,
			grpc_public_endpoint,
			geoip_json,
			updated_at_unix
		)
		VALUES (?, '', '', ?, ?)
		ON CONFLICT(scope) DO UPDATE SET
			geoip_json = excluded.geoip_json,
			updated_at_unix = excluded.updated_at_unix
	`, panelSettingsScope, string(data), toUnix(time.Now().UTC()))
	return err
}

func (s *Store) GetGeoIPSettings(ctx context.Context) (json.RawMessage, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `
		SELECT geoip_json
		FROM panel_settings
		WHERE scope = ?
	`, panelSettingsScope).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	return json.RawMessage(raw), nil
}

func (s *Store) PutGeoIPState(ctx context.Context, data json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO panel_settings (
			scope,
			http_public_url,
			grpc_public_endpoint,
			geoip_state_json,
			updated_at_unix
		)
		VALUES (?, '', '', ?, ?)
		ON CONFLICT(scope) DO UPDATE SET
			geoip_state_json = excluded.geoip_state_json,
			updated_at_unix = excluded.updated_at_unix
	`, panelSettingsScope, string(data), toUnix(time.Now().UTC()))
	return err
}

func (s *Store) GetGeoIPState(ctx context.Context) (json.RawMessage, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `
		SELECT geoip_state_json
		FROM panel_settings
		WHERE scope = ?
	`, panelSettingsScope).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	return json.RawMessage(raw), nil
}
