package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

const panelSettingsScope = "panel"

// R-Q-03: routed through dbsqlc.

func (s *Store) PutPanelSettings(ctx context.Context, settings storage.PanelSettingsRecord) error {
	return dbsqlc.New(s.db).UpsertPanelSettings(ctx, dbsqlc.UpsertPanelSettingsParams{
		Scope:              panelSettingsScope,
		HttpPublicUrl:      settings.HTTPPublicURL,
		GrpcPublicEndpoint: settings.GRPCPublicEndpoint,
		PasswordMinLength:  settings.PasswordMinLength,
		UpdatedAt:          settings.UpdatedAt.UTC(),
	})
}

func (s *Store) GetPanelSettings(ctx context.Context) (storage.PanelSettingsRecord, error) {
	row, err := dbsqlc.New(s.db).GetPanelSettings(ctx, panelSettingsScope)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.PanelSettingsRecord{}, storage.ErrNotFound
		}
		return storage.PanelSettingsRecord{}, err
	}
	return storage.PanelSettingsRecord{
		HTTPPublicURL:      row.HttpPublicUrl,
		GRPCPublicEndpoint: row.GrpcPublicEndpoint,
		PasswordMinLength:  row.PasswordMinLength,
		UpdatedAt:          row.UpdatedAt.UTC(),
	}, nil
}
