package sqlite

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
			updated_at_unix
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope) DO UPDATE SET
			http_public_url = excluded.http_public_url,
			http_root_path = excluded.http_root_path,
			grpc_public_endpoint = excluded.grpc_public_endpoint,
			http_listen_address = excluded.http_listen_address,
			grpc_listen_address = excluded.grpc_listen_address,
			tls_mode = excluded.tls_mode,
			tls_cert_file = excluded.tls_cert_file,
			tls_key_file = excluded.tls_key_file,
			updated_at_unix = excluded.updated_at_unix
	`, panelSettingsScope, settings.HTTPPublicURL, settings.HTTPRootPath, settings.GRPCPublicEndpoint, settings.HTTPListenAddress, settings.GRPCListenAddress, settings.TLSMode, settings.TLSCertFile, settings.TLSKeyFile, toUnix(settings.UpdatedAt))
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
			updated_at_unix
		FROM panel_settings
		WHERE scope = ?
	`, panelSettingsScope)

	var settings storage.PanelSettingsRecord
	var updatedAt int64
	if err := row.Scan(&settings.HTTPPublicURL, &settings.HTTPRootPath, &settings.GRPCPublicEndpoint, &settings.HTTPListenAddress, &settings.GRPCListenAddress, &settings.TLSMode, &settings.TLSCertFile, &settings.TLSKeyFile, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.PanelSettingsRecord{}, storage.ErrNotFound
		}
		return storage.PanelSettingsRecord{}, err
	}

	settings.UpdatedAt = fromUnix(updatedAt)
	return settings, nil
}
