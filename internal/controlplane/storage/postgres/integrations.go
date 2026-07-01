package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// CreateIntegrationProvider inserts a new provider row. config is opaque
// TEXT, not JSONB: fleet.Service.encryptProviderConfig may seal the
// caller-supplied JSON into a "PVS1:"/"PVS2:"/"PVS3:"-prefixed ciphertext
// string before it reaches this store (see
// db/migrations/postgres/0052_integration_providers_config_text.sql for
// why a jsonb column/cast cannot hold that). The table's CHECK constraint
// enforces "plain JSON OR PVS_:%-prefixed ciphertext" at write time in
// place of JSONB's native validation.
func (s *Store) CreateIntegrationProvider(ctx context.Context, p storage.IntegrationProviderRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO integration_providers (id, kind, label, config, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, p.ID, p.Kind, p.Label, string(p.Config), p.CreatedAt.UTC(), p.UpdatedAt.UTC())
	return err
}

// UpdateIntegrationProvider — see CreateIntegrationProvider's doc comment
// for why config binds as plain TEXT rather than a ::jsonb cast.
func (s *Store) UpdateIntegrationProvider(ctx context.Context, p storage.IntegrationProviderRecord) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE integration_providers
		SET label      = $1,
		    config     = $2,
		    updated_at = $3
		WHERE id = $4
	`, p.Label, string(p.Config), p.UpdatedAt.UTC(), p.ID)
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
	var config []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, label, config, created_at, updated_at
		FROM integration_providers
		WHERE id = $1
	`, id).Scan(&p.ID, &p.Kind, &p.Label, &config, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.IntegrationProviderRecord{}, storage.ErrNotFound
		}
		return storage.IntegrationProviderRecord{}, err
	}
	p.Config = config
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	return p, nil
}

func (s *Store) ListIntegrationProviders(ctx context.Context) ([]storage.IntegrationProviderRecord, error) {
	return s.scanIntegrationProviders(ctx, `
		SELECT id, kind, label, config, created_at, updated_at
		FROM integration_providers
		ORDER BY kind, created_at, id
	`)
}

func (s *Store) ListIntegrationProvidersByKind(ctx context.Context, kind string) ([]storage.IntegrationProviderRecord, error) {
	return s.scanIntegrationProviders(ctx, `
		SELECT id, kind, label, config, created_at, updated_at
		FROM integration_providers
		WHERE kind = $1
		ORDER BY created_at, id
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
		var config []byte
		if err := rows.Scan(&p.ID, &p.Kind, &p.Label, &config, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Config = config
		p.CreatedAt = p.CreatedAt.UTC()
		p.UpdatedAt = p.UpdatedAt.UTC()
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) DeleteIntegrationProvider(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM integration_providers WHERE id = $1`, id)
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
			(id, fleet_group_id, kind, provider_id, config, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
	`, i.ID, i.FleetGroupID, i.Kind, providerID, string(i.Config),
		i.Enabled, i.CreatedAt.UTC(), i.UpdatedAt.UTC())
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
		SET provider_id = $1,
		    config      = $2::jsonb,
		    enabled     = $3,
		    updated_at  = $4
		WHERE id = $5
	`, providerID, string(i.Config), i.Enabled, i.UpdatedAt.UTC(), i.ID)
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
	var config []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT id, fleet_group_id, kind, provider_id, config, enabled, created_at, updated_at
		FROM fleet_group_integrations
		WHERE id = $1
	`, id).Scan(&i.ID, &i.FleetGroupID, &i.Kind, &providerID, &config, &i.Enabled, &i.CreatedAt, &i.UpdatedAt)
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
	i.Config = config
	i.CreatedAt = i.CreatedAt.UTC()
	i.UpdatedAt = i.UpdatedAt.UTC()
	return i, nil
}

func (s *Store) ListFleetGroupIntegrations(ctx context.Context, fleetGroupID string) ([]storage.FleetGroupIntegrationRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, fleet_group_id, kind, provider_id, config, enabled, created_at, updated_at
		FROM fleet_group_integrations
		WHERE fleet_group_id = $1
		ORDER BY kind, created_at, id
	`, fleetGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.FleetGroupIntegrationRecord, 0)
	for rows.Next() {
		var i storage.FleetGroupIntegrationRecord
		var providerID sql.NullString
		var config []byte
		if err := rows.Scan(&i.ID, &i.FleetGroupID, &i.Kind, &providerID, &config, &i.Enabled, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		if providerID.Valid {
			pid := providerID.String
			i.ProviderID = &pid
		}
		i.Config = config
		i.CreatedAt = i.CreatedAt.UTC()
		i.UpdatedAt = i.UpdatedAt.UTC()
		result = append(result, i)
	}
	return result, rows.Err()
}

func (s *Store) DeleteFleetGroupIntegration(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM fleet_group_integrations WHERE id = $1`, id)
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
