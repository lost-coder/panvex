package sqlite

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
		INSERT INTO fleet_groups (id, name, label, description, created_at_unix, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name            = excluded.name,
			label           = excluded.label,
			description     = excluded.description,
			created_at_unix = excluded.created_at_unix,
			updated_at_unix = excluded.updated_at_unix
	`, group.ID, group.Name, group.Label, group.Description, toUnix(group.CreatedAt), toUnix(updatedAt))
	return err
}

func (s *Store) CreateFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_groups (id, name, label, description, created_at_unix, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?)
	`, group.ID, group.Name, group.Label, group.Description, toUnix(group.CreatedAt), toUnix(group.UpdatedAt))
	return err
}

// UpdateFleetGroup modifies editable fields only. `name` is the
// immutable slug and is intentionally absent from the SET list.
func (s *Store) UpdateFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE fleet_groups
		SET label           = ?,
		    description     = ?,
		    updated_at_unix = ?
		WHERE id = ?
	`, group.Label, group.Description, toUnix(group.UpdatedAt), group.ID)
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
		SELECT id, name, label, description, created_at_unix, updated_at_unix
		FROM fleet_groups
		WHERE id = ?
	`, id)
}

func (s *Store) GetFleetGroupByName(ctx context.Context, name string) (storage.FleetGroupRecord, error) {
	return s.scanFleetGroupRow(ctx, `
		SELECT id, name, label, description, created_at_unix, updated_at_unix
		FROM fleet_groups
		WHERE name = ?
	`, name)
}

func (s *Store) scanFleetGroupRow(ctx context.Context, query string, arg string) (storage.FleetGroupRecord, error) {
	var group storage.FleetGroupRecord
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, query, arg).Scan(
		&group.ID, &group.Name, &group.Label, &group.Description, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.FleetGroupRecord{}, storage.ErrNotFound
		}
		return storage.FleetGroupRecord{}, err
	}
	group.CreatedAt = fromUnix(createdAt)
	group.UpdatedAt = fromUnix(updatedAt)
	return group, nil
}

func (s *Store) ListFleetGroups(ctx context.Context) ([]storage.FleetGroupRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, label, description, created_at_unix, updated_at_unix
		FROM fleet_groups
		ORDER BY created_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.FleetGroupRecord, 0)
	for rows.Next() {
		var group storage.FleetGroupRecord
		var createdAt, updatedAt int64
		if err := rows.Scan(
			&group.ID, &group.Name, &group.Label, &group.Description, &createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		group.CreatedAt = fromUnix(createdAt)
		group.UpdatedAt = fromUnix(updatedAt)
		result = append(result, group)
	}

	return result, rows.Err()
}

func (s *Store) DeleteFleetGroup(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM fleet_groups WHERE id = ?`, id)
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
			(SELECT COUNT(*) FROM agents              WHERE fleet_group_id = ?),
			(SELECT COUNT(*) FROM enrollment_tokens   WHERE fleet_group_id = ?),
			(SELECT COUNT(*) FROM client_assignments  WHERE fleet_group_id = ?)
	`, fleetGroupID, fleetGroupID, fleetGroupID).Scan(
		&counts.Agents, &counts.EnrollmentTokens, &counts.ClientAssignments,
	)
	if err != nil {
		return storage.ReassignCounts{}, err
	}
	return counts, nil
}

// ReassignFleetGroupMembers is NOT atomic on its own — callers must
// wrap the full delete flow in Store.Transact so partial progress is
// not visible on crash. See fleet.Service.Delete.
func (s *Store) ReassignFleetGroupMembers(ctx context.Context, fromID, toID string) (storage.ReassignCounts, error) {
	var counts storage.ReassignCounts
	updates := []struct {
		stmt  string
		field *int64
	}{
		{`UPDATE agents             SET fleet_group_id = ? WHERE fleet_group_id = ?`, &counts.Agents},
		{`UPDATE enrollment_tokens  SET fleet_group_id = ? WHERE fleet_group_id = ?`, &counts.EnrollmentTokens},
		{`UPDATE client_assignments SET fleet_group_id = ? WHERE fleet_group_id = ?`, &counts.ClientAssignments},
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
