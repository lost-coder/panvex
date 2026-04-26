package postgres

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-03: routed through dbsqlc.

func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	rowsAffected, err := dbsqlc.New(s.sqlDB).DeleteUser(ctx, userID)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return storage.ErrNotFound
	}
	return nil
}
