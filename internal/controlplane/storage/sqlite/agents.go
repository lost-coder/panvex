package sqlite

import (
	"context"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

func (s *Store) DeleteAgent(ctx context.Context, agentID string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM agents
		WHERE id = ?
	`, agentID)
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

func (s *Store) UpdateAgentNodeName(ctx context.Context, agentID string, nodeName string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agents
		SET node_name = ?
		WHERE id = ?
	`, nodeName, agentID)
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

func (s *Store) DeleteInstancesByAgent(ctx context.Context, agentID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM instances
		WHERE agent_id = ?
	`, agentID)
	return err
}
