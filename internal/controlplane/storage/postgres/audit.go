package postgres

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// AppendAuditEvent persists one audit row.
//
// R-Q-03: routed through dbsqlc.AppendAuditEvent. The details field
// flows through the encodeJSON helper so legacy callers keep their
// untyped `map[string]any` shape — sqlc owns the column-level types
// for everything else.
func (s *Store) AppendAuditEvent(ctx context.Context, event storage.AuditEventRecord) error {
	if s.sqlDB == nil {
		return errTxBoundStore
	}
	detailsJSON, err := encodeJSON(event.Details)
	if err != nil {
		return err
	}
	return dbsqlc.New(s.sqlDB).AppendAuditEvent(ctx, dbsqlc.AppendAuditEventParams{
		ID:        event.ID,
		ActorID:   event.ActorID,
		Action:    event.Action,
		TargetID:  event.TargetID,
		Details:   detailsJSON,
		CreatedAt: event.CreatedAt.UTC(),
	})
}

func (s *Store) ListAuditEvents(ctx context.Context, limit int) ([]storage.AuditEventRecord, error) {
	if s.sqlDB == nil {
		return nil, errTxBoundStore
	}
	if limit <= 0 {
		limit = 1024
	}
	rows, err := dbsqlc.New(s.sqlDB).ListAuditEvents(ctx, int32(limit))
	if err != nil {
		return nil, err
	}
	result := make([]storage.AuditEventRecord, 0, len(rows))
	for _, row := range rows {
		event := storage.AuditEventRecord{
			ID:        row.ID,
			ActorID:   row.ActorID,
			Action:    row.Action,
			TargetID:  row.TargetID,
			CreatedAt: row.CreatedAt.UTC(),
		}
		if err := decodeJSON(row.Details, &event.Details); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, nil
}

// PruneAuditEvents deletes audit_events rows with created_at strictly before
// the cutoff and returns the RowsAffected count (P2-REL-04 / finding M-R2).
// Relies on idx_audit_events_created_at (added in P2-DB-02) for efficiency.
//
// R-Q-03: routed through dbsqlc.PruneAuditEvents.
func (s *Store) PruneAuditEvents(ctx context.Context, before time.Time) (int64, error) {
	if s.sqlDB == nil {
		return 0, errTxBoundStore
	}
	return dbsqlc.New(s.sqlDB).PruneAuditEvents(ctx, before.UTC())
}
