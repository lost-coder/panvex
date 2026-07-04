package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// GeoIP settings (operator-managed) and state (worker-managed) ride
// alongside the panel_settings singleton row in the geoip_json and
// geoip_state_json columns added by migration 0034. Both are treated
// as opaque JSON blobs at the storage layer; schema validation is the
// server's job.
//
// R-Q-03: routed through dbsqlc. Mirrors the retention_settings
// pattern — an empty/missing column returns (nil, nil) so callers can
// fall back to defaults.

func (s *Store) PutGeoIPSettings(ctx context.Context, data json.RawMessage) error {
	return dbsqlc.New(s.db).UpsertGeoIPJSON(ctx, dbsqlc.UpsertGeoIPJSONParams{
		Scope:     panelSettingsScope,
		GeoipJson: string(data),
		UpdatedAt: time.Now().UTC(),
	})
}

func (s *Store) GetGeoIPSettings(ctx context.Context) (json.RawMessage, error) {
	raw, err := dbsqlc.New(s.db).GetGeoIPJSON(ctx, panelSettingsScope)
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
	return dbsqlc.New(s.db).UpsertGeoIPStateJSON(ctx, dbsqlc.UpsertGeoIPStateJSONParams{
		Scope:          panelSettingsScope,
		GeoipStateJson: string(data),
		UpdatedAt:      time.Now().UTC(),
	})
}

func (s *Store) GetGeoIPState(ctx context.Context) (json.RawMessage, error) {
	raw, err := dbsqlc.New(s.db).GetGeoIPStateJSON(ctx, panelSettingsScope)
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
