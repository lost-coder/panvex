package postgres

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03: routed through dbsqlc.

func (s *Store) UpsertConsumedTotp(ctx context.Context, record storage.ConsumedTotpRecord) error {
	return dbsqlc.New(s.db).UpsertConsumedTotp(ctx, dbsqlc.UpsertConsumedTotpParams{
		UserID: record.UserID,
		Code:   record.Code,
		UsedAt: record.UsedAt.UTC(),
	})
}

func (s *Store) ListConsumedTotp(ctx context.Context) ([]storage.ConsumedTotpRecord, error) {
	rows, err := dbsqlc.New(s.db).ListConsumedTotp(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]storage.ConsumedTotpRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.ConsumedTotpRecord{
			UserID: row.UserID,
			Code:   row.Code,
			UsedAt: row.UsedAt.UTC(),
		})
	}
	return out, nil
}

func (s *Store) DeleteExpiredConsumedTotp(ctx context.Context, before time.Time) error {
	return dbsqlc.New(s.db).DeleteExpiredConsumedTotp(ctx, before.UTC())
}
