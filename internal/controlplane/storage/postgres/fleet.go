package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	updatedAt := group.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = group.CreatedAt
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_groups (id, name, label, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE
		SET name        = EXCLUDED.name,
		    label       = EXCLUDED.label,
		    description = EXCLUDED.description,
		    created_at  = EXCLUDED.created_at,
		    updated_at  = EXCLUDED.updated_at
	`, group.ID, group.Name, group.Label, group.Description,
		group.CreatedAt.UTC(), updatedAt.UTC())
	return err
}

func (s *Store) CreateFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_groups (id, name, label, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, group.ID, group.Name, group.Label, group.Description,
		group.CreatedAt.UTC(), group.UpdatedAt.UTC())
	return err
}

// UpdateFleetGroup mutates editable fields only; `name` is the
// immutable slug and is not in the SET list.
func (s *Store) UpdateFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE fleet_groups
		SET label       = $1,
		    description = $2,
		    updated_at  = $3
		WHERE id = $4
	`, group.Label, group.Description, group.UpdatedAt.UTC(), group.ID)
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

func (s *Store) GetFleetGroup(ctx context.Context, id string) (storage.FleetGroupRecord, error) {
	return s.scanFleetGroupRow(ctx, `
		SELECT id, name, label, description, created_at, updated_at
		FROM fleet_groups
		WHERE id = $1
	`, id)
}

func (s *Store) GetFleetGroupByName(ctx context.Context, name string) (storage.FleetGroupRecord, error) {
	return s.scanFleetGroupRow(ctx, `
		SELECT id, name, label, description, created_at, updated_at
		FROM fleet_groups
		WHERE name = $1
	`, name)
}

func (s *Store) scanFleetGroupRow(ctx context.Context, query string, arg string) (storage.FleetGroupRecord, error) {
	var group storage.FleetGroupRecord
	err := s.db.QueryRowContext(ctx, query, arg).Scan(
		&group.ID, &group.Name, &group.Label, &group.Description,
		&group.CreatedAt, &group.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.FleetGroupRecord{}, storage.ErrNotFound
		}
		return storage.FleetGroupRecord{}, err
	}
	group.CreatedAt = group.CreatedAt.UTC()
	group.UpdatedAt = group.UpdatedAt.UTC()
	return group, nil
}

func (s *Store) ListFleetGroups(ctx context.Context) ([]storage.FleetGroupRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, label, description, created_at, updated_at
		FROM fleet_groups
		ORDER BY created_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.FleetGroupRecord, 0)
	for rows.Next() {
		var group storage.FleetGroupRecord
		if err := rows.Scan(
			&group.ID, &group.Name, &group.Label, &group.Description,
			&group.CreatedAt, &group.UpdatedAt,
		); err != nil {
			return nil, err
		}
		group.CreatedAt = group.CreatedAt.UTC()
		group.UpdatedAt = group.UpdatedAt.UTC()
		result = append(result, group)
	}

	return result, rows.Err()
}

func (s *Store) DeleteFleetGroup(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM fleet_groups WHERE id = $1`, id)
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

func (s *Store) CountFleetGroupMembers(ctx context.Context, fleetGroupID string) (storage.ReassignCounts, error) {
	var counts storage.ReassignCounts
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM agents              WHERE fleet_group_id = $1),
			(SELECT COUNT(*) FROM enrollment_tokens   WHERE fleet_group_id = $1),
			(SELECT COUNT(*) FROM client_assignments  WHERE fleet_group_id = $1)
	`, fleetGroupID).Scan(
		&counts.Agents, &counts.EnrollmentTokens, &counts.ClientAssignments,
	)
	if err != nil {
		return storage.ReassignCounts{}, err
	}
	return counts, nil
}

// ReassignFleetGroupMembers is NOT atomic on its own — callers must
// wrap the full delete flow in Store.Transact. See fleet.Service.Delete.
func (s *Store) ReassignFleetGroupMembers(ctx context.Context, fromID, toID string) (storage.ReassignCounts, error) {
	var counts storage.ReassignCounts
	updates := []struct {
		stmt  string
		field *int64
	}{
		{`UPDATE agents             SET fleet_group_id = $1 WHERE fleet_group_id = $2`, &counts.Agents},
		{`UPDATE enrollment_tokens  SET fleet_group_id = $1 WHERE fleet_group_id = $2`, &counts.EnrollmentTokens},
		{`UPDATE client_assignments SET fleet_group_id = $1 WHERE fleet_group_id = $2`, &counts.ClientAssignments},
	}
	for _, u := range updates {
		result, err := s.db.ExecContext(ctx, u.stmt, toID, fromID)
		if err != nil {
			return storage.ReassignCounts{}, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return storage.ReassignCounts{}, err
		}
		*u.field = n
	}
	return counts, nil
}
