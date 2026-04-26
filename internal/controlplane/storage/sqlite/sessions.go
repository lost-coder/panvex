package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) PutSession(ctx context.Context, session storage.SessionRecord) error {
	lastSeen := session.LastSeenAt
	if lastSeen.IsZero() {
		lastSeen = session.CreatedAt
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, created_at_unix, last_seen_at_unix)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			user_id = excluded.user_id,
			created_at_unix = excluded.created_at_unix,
			last_seen_at_unix = excluded.last_seen_at_unix
	`, session.ID, session.UserID, toUnix(session.CreatedAt), toUnix(lastSeen))
	return err
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (storage.SessionRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, created_at_unix, last_seen_at_unix
		FROM sessions
		WHERE id = ?
	`, sessionID)

	var session storage.SessionRecord
	var createdAt, lastSeenAt int64
	if err := row.Scan(&session.ID, &session.UserID, &createdAt, &lastSeenAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.SessionRecord{}, storage.ErrNotFound
		}
		return storage.SessionRecord{}, err
	}

	session.CreatedAt = fromUnix(createdAt)
	session.LastSeenAt = fromUnix(lastSeenAt)
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
		SELECT id, user_id, created_at_unix, last_seen_at_unix
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
		var createdAt, lastSeenAt int64
		if err := rows.Scan(&session.ID, &session.UserID, &createdAt, &lastSeenAt); err != nil {
			return nil, err
		}
		session.CreatedAt = fromUnix(createdAt)
		session.LastSeenAt = fromUnix(lastSeenAt)
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE created_at_unix < ?`, toUnix(before))
	return err
}

// TouchSession updates only last_seen_at so the sliding idle timeout
// survives restart (Q2.U-S-12). The narrow UPDATE avoids contention on
// the rest of the row while keeping the cost per refresh minimal.
func (s *Store) TouchSession(ctx context.Context, sessionID string, lastSeenAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET last_seen_at_unix = ? WHERE id = ?
	`, toUnix(lastSeenAt), sessionID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return storage.ErrNotFound
	}
	return nil
}
