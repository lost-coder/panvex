package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

func (s *Store) PutSession(ctx context.Context, session storage.SessionRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, created_at_unix)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			user_id = excluded.user_id,
			created_at_unix = excluded.created_at_unix
	`, session.ID, session.UserID, toUnix(session.CreatedAt))
	return err
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (storage.SessionRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, created_at_unix
		FROM sessions
		WHERE id = ?
	`, sessionID)

	var session storage.SessionRecord
	var createdAt int64
	if err := row.Scan(&session.ID, &session.UserID, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.SessionRecord{}, storage.ErrNotFound
		}
		return storage.SessionRecord{}, err
	}

	session.CreatedAt = fromUnix(createdAt)
	return session, nil
}

func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	if err != nil {
		return err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) ListSessions(ctx context.Context) ([]storage.SessionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, created_at_unix
		FROM sessions
		ORDER BY created_at_unix DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []storage.SessionRecord
	for rows.Next() {
		var session storage.SessionRecord
		var createdAt int64
		if err := rows.Scan(&session.ID, &session.UserID, &createdAt); err != nil {
			return nil, err
		}
		session.CreatedAt = fromUnix(createdAt)
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE created_at_unix < ?`, toUnix(before))
	return err
}
