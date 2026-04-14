package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutSession(ctx context.Context, session storage.SessionRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT(id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			created_at = EXCLUDED.created_at
	`, session.ID, session.UserID, session.CreatedAt.UTC())
	return err
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (storage.SessionRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, created_at
		FROM sessions
		WHERE id = $1
	`, sessionID)

	var session storage.SessionRecord
	if err := row.Scan(&session.ID, &session.UserID, &session.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.SessionRecord{}, storage.ErrNotFound
		}
		return storage.SessionRecord{}, err
	}

	session.CreatedAt = session.CreatedAt.UTC()
	return session, nil
}

func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
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
		SELECT id, user_id, created_at
		FROM sessions
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []storage.SessionRecord
	for rows.Next() {
		var session storage.SessionRecord
		if err := rows.Scan(&session.ID, &session.UserID, &session.CreatedAt); err != nil {
			return nil, err
		}
		session.CreatedAt = session.CreatedAt.UTC()
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE created_at < $1`, before.UTC())
	return err
}
