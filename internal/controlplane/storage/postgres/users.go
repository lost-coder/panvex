package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutUser(ctx context.Context, user storage.UserRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, totp_enabled, totp_secret, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE
		SET username = EXCLUDED.username,
		    password_hash = EXCLUDED.password_hash,
		    role = EXCLUDED.role,
		    totp_enabled = EXCLUDED.totp_enabled,
		    totp_secret = EXCLUDED.totp_secret,
		    created_at = EXCLUDED.created_at
	`, user.ID, user.Username, user.PasswordHash, user.Role, user.TotpEnabled, user.TotpSecret, user.CreatedAt.UTC())
	return err
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (storage.UserRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at
		FROM users
		WHERE id = $1
	`, userID)

	var user storage.UserRecord
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &user.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.UserRecord{}, storage.ErrNotFound
		}
		return storage.UserRecord{}, err
	}

	user.CreatedAt = user.CreatedAt.UTC()
	return user, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (storage.UserRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at
		FROM users
		WHERE username = $1
	`, username)

	var user storage.UserRecord
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &user.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.UserRecord{}, storage.ErrNotFound
		}
		return storage.UserRecord{}, err
	}

	user.CreatedAt = user.CreatedAt.UTC()
	return user, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]storage.UserRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at
		FROM users
		ORDER BY created_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.UserRecord, 0)
	for rows.Next() {
		var user storage.UserRecord
		if err := rows.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.TotpEnabled, &user.TotpSecret, &user.CreatedAt); err != nil {
			return nil, err
		}
		user.CreatedAt = user.CreatedAt.UTC()
		result = append(result, user)
	}

	return result, rows.Err()
}
