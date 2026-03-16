package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

const panelSettingsScope = "panel"

func (s *Store) PutPanelSettings(ctx context.Context, settings storage.PanelSettingsRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO panel_settings (
			scope,
			http_public_url,
			http_root_path,
			grpc_public_endpoint,
			http_listen_address,
			grpc_listen_address,
			tls_mode,
			tls_cert_file,
			tls_key_file,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (scope) DO UPDATE
		SET http_public_url = EXCLUDED.http_public_url,
		    http_root_path = EXCLUDED.http_root_path,
		    grpc_public_endpoint = EXCLUDED.grpc_public_endpoint,
		    http_listen_address = EXCLUDED.http_listen_address,
		    grpc_listen_address = EXCLUDED.grpc_listen_address,
		    tls_mode = EXCLUDED.tls_mode,
		    tls_cert_file = EXCLUDED.tls_cert_file,
		    tls_key_file = EXCLUDED.tls_key_file,
		    updated_at = EXCLUDED.updated_at
	`, panelSettingsScope, settings.HTTPPublicURL, settings.HTTPRootPath, settings.GRPCPublicEndpoint, settings.HTTPListenAddress, settings.GRPCListenAddress, settings.TLSMode, settings.TLSCertFile, settings.TLSKeyFile, settings.UpdatedAt.UTC())
	return err
}

func (s *Store) GetPanelSettings(ctx context.Context) (storage.PanelSettingsRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			http_public_url,
			http_root_path,
			grpc_public_endpoint,
			http_listen_address,
			grpc_listen_address,
			tls_mode,
			tls_cert_file,
			tls_key_file,
			updated_at
		FROM panel_settings
		WHERE scope = $1
	`, panelSettingsScope)

	var settings storage.PanelSettingsRecord
	if err := row.Scan(&settings.HTTPPublicURL, &settings.HTTPRootPath, &settings.GRPCPublicEndpoint, &settings.HTTPListenAddress, &settings.GRPCListenAddress, &settings.TLSMode, &settings.TLSCertFile, &settings.TLSKeyFile, &settings.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.PanelSettingsRecord{}, storage.ErrNotFound
		}
		return storage.PanelSettingsRecord{}, err
	}

	settings.UpdatedAt = settings.UpdatedAt.UTC()
	return settings, nil
}
