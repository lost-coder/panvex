package postgres

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

func toNullableTime(t *time.Time) sql.NullTime {
	if t == nil || t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

// failuresToInt32 clamps a host-int failure counter into the int32 column
// width. The legitimate range is small (lockout fires after a handful of
// failures), so saturation is preferable to silent wrap if a caller ever
// passes a corrupt value.
func failuresToInt32(n int) int32 {
	if n < 0 {
		return 0
	}
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n)
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
	return dbsqlc.New(s.db).UpsertLoginLockout(ctx, dbsqlc.UpsertLoginLockoutParams{
		Username:  record.Username,
		Failures:  failuresToInt32(record.Failures),
		LockedAt:  toNullableTime(record.LockedAt),
		UpdatedAt: record.UpdatedAt.UTC(),
	})
}

func (s *Store) GetLoginLockout(ctx context.Context, username string) (storage.LoginLockoutRecord, error) {
	row, err := dbsqlc.New(s.db).GetLoginLockout(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.LoginLockoutRecord{}, storage.ErrNotFound
		}
		return storage.LoginLockoutRecord{}, err
	}
	return loginLockoutFromRow(row), nil
}

func (s *Store) DeleteLoginLockout(ctx context.Context, username string) error {
	return dbsqlc.New(s.db).DeleteLoginLockout(ctx, username)
}

func (s *Store) ListLoginLockouts(ctx context.Context) ([]storage.LoginLockoutRecord, error) {
	rows, err := dbsqlc.New(s.db).ListLoginLockouts(ctx)
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
	return dbsqlc.New(s.db).DeleteExpiredLoginLockouts(ctx, before.UTC())
}
