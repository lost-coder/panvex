package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03: routed through dbsqlc so the column-level types are
// compile-time-checked.

func (s *Store) GetCPSecret(ctx context.Context, key string) ([]byte, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	value, err := dbsqlc.New(s.sqlDB).GetCPSecret(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return value, nil
}

func (s *Store) PutCPSecret(ctx context.Context, key string, value []byte) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).UpsertCPSecret(ctx, dbsqlc.UpsertCPSecretParams{
		Key:       key,
		Value:     value,
		UpdatedAt: time.Now().UTC(),
	})
}
