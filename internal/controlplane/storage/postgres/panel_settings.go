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
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).UpsertPanelSettings(ctx, dbsqlc.UpsertPanelSettingsParams{
		Scope:              panelSettingsScope,
		HttpPublicUrl:      settings.HTTPPublicURL,
		GrpcPublicEndpoint: settings.GRPCPublicEndpoint,
		PasswordMinLength:  settings.PasswordMinLength,
		UpdatedAt:          settings.UpdatedAt.UTC(),
	})
}

func (s *Store) GetPanelSettings(ctx context.Context) (storage.PanelSettingsRecord, error) {
	if s.sqlDB == nil {
		return storage.PanelSettingsRecord{}, errTxBoundStore
	}
	row, err := dbsqlc.New(s.sqlDB).GetPanelSettings(ctx, panelSettingsScope)
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
