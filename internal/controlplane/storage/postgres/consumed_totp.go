package postgres

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03: routed through dbsqlc.

func (s *Store) UpsertConsumedTotp(ctx context.Context, record storage.ConsumedTotpRecord) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).UpsertConsumedTotp(ctx, dbsqlc.UpsertConsumedTotpParams{
		UserID: record.UserID,
		Code:   record.Code,
		UsedAt: record.UsedAt.UTC(),
	})
}

func (s *Store) ListConsumedTotp(ctx context.Context) ([]storage.ConsumedTotpRecord, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	rows, err := dbsqlc.New(s.sqlDB).ListConsumedTotp(ctx)
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
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).DeleteExpiredConsumedTotp(ctx, before.UTC())
}
