package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

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

func (s *Store) UpsertLoginLockout(ctx context.Context, record storage.LoginLockoutRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO login_lockouts (username, failures, locked_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (username) DO UPDATE SET
			failures = EXCLUDED.failures,
			locked_at = EXCLUDED.locked_at,
			updated_at = EXCLUDED.updated_at
	`,
		record.Username,
		record.Failures,
		toNullableTime(record.LockedAt),
		record.UpdatedAt.UTC(),
	)
	return err
}

func (s *Store) GetLoginLockout(ctx context.Context, username string) (storage.LoginLockoutRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT username, failures, locked_at, updated_at
		FROM login_lockouts
		WHERE username = $1
	`, username)

	var record storage.LoginLockoutRecord
	var lockedAt sql.NullTime
	if err := row.Scan(&record.Username, &record.Failures, &lockedAt, &record.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.LoginLockoutRecord{}, storage.ErrNotFound
		}
		return storage.LoginLockoutRecord{}, err
	}
	record.LockedAt = fromNullableTime(lockedAt)
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record, nil
}

func (s *Store) DeleteLoginLockout(ctx context.Context, username string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_lockouts WHERE username = $1`, username)
	return err
}

func (s *Store) ListLoginLockouts(ctx context.Context) ([]storage.LoginLockoutRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT username, failures, locked_at, updated_at
		FROM login_lockouts
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]storage.LoginLockoutRecord, 0)
	for rows.Next() {
		var record storage.LoginLockoutRecord
		var lockedAt sql.NullTime
		if err := rows.Scan(&record.Username, &record.Failures, &lockedAt, &record.UpdatedAt); err != nil {
			return nil, err
		}
		record.LockedAt = fromNullableTime(lockedAt)
		record.UpdatedAt = record.UpdatedAt.UTC()
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) DeleteExpiredLoginLockouts(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM login_lockouts WHERE updated_at < $1`,
		before.UTC(),
	)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}
