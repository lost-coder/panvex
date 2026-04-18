package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func toNullableUnix(t *time.Time) sql.NullInt64 {
	if t == nil || t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UTC().Unix(), Valid: true}
}

func fromNullableUnix(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromUnix(v.Int64)
	return &t
}

func (s *Store) UpsertLoginLockout(ctx context.Context, record storage.LoginLockoutRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO login_lockouts (username, failures, locked_at_unix, updated_at_unix)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			failures = excluded.failures,
			locked_at_unix = excluded.locked_at_unix,
			updated_at_unix = excluded.updated_at_unix
	`,
		record.Username,
		record.Failures,
		toNullableUnix(record.LockedAt),
		toUnix(record.UpdatedAt),
	)
	return err
}

func (s *Store) GetLoginLockout(ctx context.Context, username string) (storage.LoginLockoutRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT username, failures, locked_at_unix, updated_at_unix
		FROM login_lockouts
		WHERE username = ?
	`, username)

	var record storage.LoginLockoutRecord
	var lockedAt sql.NullInt64
	var updatedAt int64
	if err := row.Scan(&record.Username, &record.Failures, &lockedAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.LoginLockoutRecord{}, storage.ErrNotFound
		}
		return storage.LoginLockoutRecord{}, err
	}
	record.LockedAt = fromNullableUnix(lockedAt)
	record.UpdatedAt = fromUnix(updatedAt)
	return record, nil
}

func (s *Store) DeleteLoginLockout(ctx context.Context, username string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_lockouts WHERE username = ?`, username)
	return err
}

func (s *Store) ListLoginLockouts(ctx context.Context) ([]storage.LoginLockoutRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT username, failures, locked_at_unix, updated_at_unix
		FROM login_lockouts
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]storage.LoginLockoutRecord, 0)
	for rows.Next() {
		var record storage.LoginLockoutRecord
		var lockedAt sql.NullInt64
		var updatedAt int64
		if err := rows.Scan(&record.Username, &record.Failures, &lockedAt, &updatedAt); err != nil {
			return nil, err
		}
		record.LockedAt = fromNullableUnix(lockedAt)
		record.UpdatedAt = fromUnix(updatedAt)
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) DeleteExpiredLoginLockouts(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM login_lockouts WHERE updated_at_unix < ?`,
		toUnix(before),
	)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}
