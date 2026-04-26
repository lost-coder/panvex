package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03: routed through dbsqlc.

func toNullableTime(t *time.Time) sql.NullTime {
	if t == nil || t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

func fromNullableTime(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}

func loginLockoutFromRow(row dbsqlc.LoginLockout) storage.LoginLockoutRecord {
	return storage.LoginLockoutRecord{
		Username:  row.Username,
		Failures:  int(row.Failures),
		LockedAt:  fromNullableTime(row.LockedAt),
		UpdatedAt: row.UpdatedAt.UTC(),
	}
}

func (s *Store) UpsertLoginLockout(ctx context.Context, record storage.LoginLockoutRecord) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).UpsertLoginLockout(ctx, dbsqlc.UpsertLoginLockoutParams{
		Username:  record.Username,
		Failures:  int32(record.Failures),
		LockedAt:  toNullableTime(record.LockedAt),
		UpdatedAt: record.UpdatedAt.UTC(),
	})
}

func (s *Store) GetLoginLockout(ctx context.Context, username string) (storage.LoginLockoutRecord, error) {
	if s.sqlDB == nil {
		return storage.LoginLockoutRecord{}, errTxBoundStore
	}
	row, err := dbsqlc.New(s.sqlDB).GetLoginLockout(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.LoginLockoutRecord{}, storage.ErrNotFound
		}
		return storage.LoginLockoutRecord{}, err
	}
	return loginLockoutFromRow(row), nil
}

func (s *Store) DeleteLoginLockout(ctx context.Context, username string) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).DeleteLoginLockout(ctx, username)
}

func (s *Store) ListLoginLockouts(ctx context.Context) ([]storage.LoginLockoutRecord, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	rows, err := dbsqlc.New(s.sqlDB).ListLoginLockouts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]storage.LoginLockoutRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, loginLockoutFromRow(row))
	}
	return out, nil
}

func (s *Store) DeleteExpiredLoginLockouts(ctx context.Context, before time.Time) (int64, error) {
	if s.sqlDB == nil {
		return 0, errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).DeleteExpiredLoginLockouts(ctx, before.UTC())
}
