package postgres

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) AppendAuditEvent(ctx context.Context, event storage.AuditEventRecord) error {
	detailsJSON, err := encodeJSON(event.Details)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO audit_events (id, actor_id, action, target_id, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, event.ID, event.ActorID, event.Action, event.TargetID, detailsJSON, event.CreatedAt.UTC())
	return err
}

func (s *Store) ListAuditEvents(ctx context.Context, limit int) ([]storage.AuditEventRecord, error) {
	if limit <= 0 {
		limit = 1024
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_id, action, target_id, details, created_at
		FROM (SELECT * FROM audit_events ORDER BY created_at DESC, id DESC LIMIT $1) sub
		ORDER BY created_at, id
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AuditEventRecord, 0)
	for rows.Next() {
		var event storage.AuditEventRecord
		var detailsJSON []byte
		if err := rows.Scan(&event.ID, &event.ActorID, &event.Action, &event.TargetID, &detailsJSON, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.CreatedAt = event.CreatedAt.UTC()
		if err := decodeJSON(detailsJSON, &event.Details); err != nil {
			return nil, err
		}
		result = append(result, event)
	}

	return result, rows.Err()
}

// PruneAuditEvents deletes audit_events rows with created_at strictly before
// the cutoff and returns the RowsAffected count (P2-REL-04 / finding M-R2).
// Relies on idx_audit_events_created_at (added in P2-DB-02) for efficiency.
func (s *Store) PruneAuditEvents(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM audit_events WHERE created_at < $1`, before.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
