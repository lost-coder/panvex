package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// PutAgentFallbackState marks the agent as having entered ME→Direct fallback
// at rec.EnteredAt. The first writer wins: subsequent calls while a row is
// already present are a no-op so the original transition timestamp survives
// across observer churn (Phase 4 §4.3).
func (s *Store) PutAgentFallbackState(ctx context.Context, rec storage.AgentFallbackStateRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_fallback_state (agent_id, entered_at_unix)
		VALUES ($1, $2)
		ON CONFLICT (agent_id) DO NOTHING
	`, rec.AgentID, rec.EnteredAt.Unix())
	return err
}

// DeleteAgentFallbackState clears the fallback marker — invoked when the
// agent's MERuntimeReady flag returns to true. Idempotent.
func (s *Store) DeleteAgentFallbackState(ctx context.Context, agentID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_fallback_state WHERE agent_id = $1`, agentID)
	return err
}

// GetAgentFallbackState returns the persisted fallback entry for one agent.
// Returns storage.ErrNotFound when the agent is not currently in fallback.
func (s *Store) GetAgentFallbackState(ctx context.Context, agentID string) (storage.AgentFallbackStateRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT agent_id, entered_at_unix FROM agent_fallback_state WHERE agent_id = $1
	`, agentID)
	var (
		id string
		ts int64
	)
	if err := row.Scan(&id, &ts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.AgentFallbackStateRecord{}, storage.ErrNotFound
		}
		return storage.AgentFallbackStateRecord{}, err
	}
	return storage.AgentFallbackStateRecord{
		AgentID:   id,
		EnteredAt: time.Unix(ts, 0).UTC(),
	}, nil
}

// ListAgentFallbackState returns every agent currently flagged as in
// fallback, oldest entry first. Drives the panel-side severity refresh
// loop on startup.
func (s *Store) ListAgentFallbackState(ctx context.Context) ([]storage.AgentFallbackStateRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, entered_at_unix FROM agent_fallback_state ORDER BY entered_at_unix ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []storage.AgentFallbackStateRecord
	for rows.Next() {
		var (
			id string
			ts int64
		)
		if err := rows.Scan(&id, &ts); err != nil {
			return nil, err
		}
		out = append(out, storage.AgentFallbackStateRecord{
			AgentID:   id,
			EnteredAt: time.Unix(ts, 0).UTC(),
		})
	}
	return out, rows.Err()
}
