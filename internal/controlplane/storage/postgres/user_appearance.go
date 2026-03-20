package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

func (s *Store) PutUserAppearance(ctx context.Context, appearance storage.UserAppearanceRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_appearance (user_id, theme, density, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE
		SET theme = EXCLUDED.theme,
		    density = EXCLUDED.density,
		    updated_at = EXCLUDED.updated_at
	`, appearance.UserID, appearance.Theme, appearance.Density, appearance.UpdatedAt.UTC())
	return err
}

func (s *Store) GetUserAppearance(ctx context.Context, userID string) (storage.UserAppearanceRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT user_id, theme, density, updated_at
		FROM user_appearance
		WHERE user_id = $1
	`, userID)

	var appearance storage.UserAppearanceRecord
	if err := row.Scan(&appearance.UserID, &appearance.Theme, &appearance.Density, &appearance.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return defaultUserAppearanceRecord(userID), nil
		}
		return storage.UserAppearanceRecord{}, err
	}

	appearance.UpdatedAt = appearance.UpdatedAt.UTC()
	return appearance, nil
}

func (s *Store) ListUserAppearances(ctx context.Context) ([]storage.UserAppearanceRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, theme, density, updated_at
		FROM user_appearance
		ORDER BY user_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	appearances := make([]storage.UserAppearanceRecord, 0)
	for rows.Next() {
		var appearance storage.UserAppearanceRecord
		if err := rows.Scan(&appearance.UserID, &appearance.Theme, &appearance.Density, &appearance.UpdatedAt); err != nil {
			return nil, err
		}
		appearance.UpdatedAt = appearance.UpdatedAt.UTC()
		appearances = append(appearances, appearance)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return appearances, nil
}

func defaultUserAppearanceRecord(userID string) storage.UserAppearanceRecord {
	return storage.UserAppearanceRecord{
		UserID:  userID,
		Theme:   "system",
		Density: "comfortable",
	}
}
