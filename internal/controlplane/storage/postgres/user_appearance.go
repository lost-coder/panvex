package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03: routed through dbsqlc.

func userAppearanceFromRow(row dbsqlc.UserAppearance) storage.UserAppearanceRecord {
	return storage.UserAppearanceRecord{
		UserID:    row.UserID,
		Theme:     row.Theme,
		Density:   row.Density,
		HelpMode:  row.HelpMode,
		UpdatedAt: row.UpdatedAt.UTC(),
	}
}

func (s *Store) PutUserAppearance(ctx context.Context, appearance storage.UserAppearanceRecord) error {
	return dbsqlc.New(s.db).UpsertUserAppearance(ctx, dbsqlc.UpsertUserAppearanceParams{
		UserID:    appearance.UserID,
		Theme:     appearance.Theme,
		Density:   appearance.Density,
		HelpMode:  appearance.HelpMode,
		UpdatedAt: appearance.UpdatedAt.UTC(),
	})
}

func (s *Store) GetUserAppearance(ctx context.Context, userID string) (storage.UserAppearanceRecord, error) {
	row, err := dbsqlc.New(s.db).GetUserAppearance(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return defaultUserAppearanceRecord(userID), nil
		}
		return storage.UserAppearanceRecord{}, err
	}
	return userAppearanceFromRow(row), nil
}

func (s *Store) ListUserAppearances(ctx context.Context) ([]storage.UserAppearanceRecord, error) {
	rows, err := dbsqlc.New(s.db).ListUserAppearances(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]storage.UserAppearanceRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, userAppearanceFromRow(row))
	}
	return out, nil
}

func defaultUserAppearanceRecord(userID string) storage.UserAppearanceRecord {
	return storage.UserAppearanceRecord{
		UserID:   userID,
		Theme:    "system",
		Density:  "comfortable",
		HelpMode: "basic",
	}
}
