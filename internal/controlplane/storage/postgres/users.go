package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// PutUser upserts one users row.
//
// R-Q-03: routed through dbsqlc.UpsertUser. The created_at column is no
// longer touched by the upsert path so an UPDATE keeps the original
// timestamp — this matches the prior behaviour where ON CONFLICT set
// created_at to EXCLUDED.created_at and callers passed the same value
// they originally inserted; the column is stable across upserts so
// dropping it from the SET keeps the existing semantic for every
// observed callsite.
func (s *Store) PutUser(ctx context.Context, user storage.UserRecord) error {
	return dbsqlc.New(s.db).UpsertUser(ctx, dbsqlc.UpsertUserParams{
		ID:           user.ID,
		Username:     user.Username,
		PasswordHash: user.PasswordHash,
		Role:         user.Role,
		TotpEnabled:  user.TotpEnabled,
		TotpSecret:   user.TotpSecret,
		CreatedAt:    user.CreatedAt.UTC(),
	})
}

func userRecordFromRow(row dbsqlc.User) storage.UserRecord {
	return storage.UserRecord{
		ID:           row.ID,
		Username:     row.Username,
		PasswordHash: row.PasswordHash,
		Role:         row.Role,
		TotpEnabled:  row.TotpEnabled,
		TotpSecret:   row.TotpSecret,
		CreatedAt:    row.CreatedAt.UTC(),
	}
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (storage.UserRecord, error) {
	row, err := dbsqlc.New(s.db).GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.UserRecord{}, storage.ErrNotFound
		}
		return storage.UserRecord{}, err
	}
	return userRecordFromRow(row), nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (storage.UserRecord, error) {
	row, err := dbsqlc.New(s.db).GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.UserRecord{}, storage.ErrNotFound
		}
		return storage.UserRecord{}, err
	}
	return userRecordFromRow(row), nil
}

func (s *Store) ListUsers(ctx context.Context) ([]storage.UserRecord, error) {
	rows, err := dbsqlc.New(s.db).ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]storage.UserRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, userRecordFromRow(row))
	}
	return result, nil
}
