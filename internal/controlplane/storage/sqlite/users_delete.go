package sqlite

import (
	"context"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM users
		WHERE id = ?
	`, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return storage.ErrNotFound
	}

	return nil
}
