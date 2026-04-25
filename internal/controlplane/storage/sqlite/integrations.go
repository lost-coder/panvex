package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

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
