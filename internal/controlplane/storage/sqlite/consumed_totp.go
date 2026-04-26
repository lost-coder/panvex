package sqlite

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// UpsertConsumedTotp inserts (or refreshes used_at on conflict) a
// consumed-TOTP row so a replayed code is rejected after restart
// (Q2.U-S-17).
func (s *Store) UpsertConsumedTotp(ctx context.Context, record storage.ConsumedTotpRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO consumed_totp (user_id, code, used_at_unix)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, code) DO UPDATE SET used_at_unix = excluded.used_at_unix
	`, record.UserID, record.Code, toUnix(record.UsedAt))
	return err
}

// ListConsumedTotp returns every persisted (user_id, code) pair so the
// auth service can rebuild its in-memory replay map on startup.
func (s *Store) ListConsumedTotp(ctx context.Context) ([]storage.ConsumedTotpRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, code, used_at_unix FROM consumed_totp
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storage.ConsumedTotpRecord
	for rows.Next() {
		var rec storage.ConsumedTotpRecord
		var usedAt int64
		if err := rows.Scan(&rec.UserID, &rec.Code, &usedAt); err != nil {
			return nil, err
		}
		rec.UsedAt = fromUnix(usedAt)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) DeleteExpiredConsumedTotp(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM consumed_totp WHERE used_at_unix < ?`, toUnix(before))
	return err
}
