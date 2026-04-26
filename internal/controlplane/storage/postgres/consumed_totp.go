package postgres

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) UpsertConsumedTotp(ctx context.Context, record storage.ConsumedTotpRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO consumed_totp (user_id, code, used_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, code) DO UPDATE SET used_at = EXCLUDED.used_at
	`, record.UserID, record.Code, record.UsedAt.UTC())
	return err
}

func (s *Store) ListConsumedTotp(ctx context.Context) ([]storage.ConsumedTotpRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, code, used_at FROM consumed_totp
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storage.ConsumedTotpRecord
	for rows.Next() {
		var rec storage.ConsumedTotpRecord
		if err := rows.Scan(&rec.UserID, &rec.Code, &rec.UsedAt); err != nil {
			return nil, err
		}
		rec.UsedAt = rec.UsedAt.UTC()
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) DeleteExpiredConsumedTotp(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM consumed_totp WHERE used_at < $1`, before.UTC())
	return err
}
