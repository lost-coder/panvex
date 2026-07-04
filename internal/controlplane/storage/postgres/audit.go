package postgres

import (
	"context"
	"database/sql"
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
	detailsJSON, err := encodeJSON(event.Details)
	if err != nil {
		return err
	}
	return dbsqlc.New(s.db).AppendAuditEvent(ctx, dbsqlc.AppendAuditEventParams{
		ID:        event.ID,
		ActorID:   event.ActorID,
		Action:    event.Action,
		TargetID:  event.TargetID,
		Details:   detailsJSON,
		CreatedAt: event.CreatedAt.UTC(),
		PrevHash:  event.PrevHash,
		EventHash: event.EventHash,
	})
}

// LatestAuditChainHash returns the EventHash of the most recently
// persisted audit row. Empty string when the table is empty.
//
// Producers read this once per batch flush so each row is chained
// onto the tail of the existing chain. See AuditStore.LatestAuditChainHash.
func (s *Store) LatestAuditChainHash(ctx context.Context) (string, error) {
	return dbsqlc.New(s.db).LatestAuditChainHash(ctx)
}

func (s *Store) ListAuditEvents(ctx context.Context, limit int) ([]storage.AuditEventRecord, error) {
	if limit <= 0 {
		limit = 1024
	}
	rows, err := dbsqlc.New(s.db).ListAuditEvents(ctx, int32(limit))
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
			PrevHash:  row.PrevHash,
			EventHash: row.EventHash,
		}
		if err := decodeJSON(row.Details, &event.Details); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, nil
}

// ListAuditEventsCursor returns one keyset-paginated page in (created_at
// DESC, id DESC) order — newest first. Hand-written SQL (not sqlc) so the
// tuple-comparison form ports cleanly across drivers without regenerating
// the entire dbsqlc tree. See storage.AuditStore for the contract.
func (s *Store) ListAuditEventsCursor(ctx context.Context, params storage.ListAuditEventsCursorParams) ([]storage.AuditEventRecord, storage.ListAuditEventsCursorParams, error) {
	limit := storage.NormalizeCursorLimit(params.Limit)

	var rows *sql.Rows
	var err error
	if params.AfterID == "" && params.AfterCreatedAt.IsZero() {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, actor_id, action, target_id, details, created_at, prev_hash, event_hash
			FROM audit_events
			ORDER BY created_at DESC, id DESC
			LIMIT $1
		`, limit+1)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, actor_id, action, target_id, details, created_at, prev_hash, event_hash
			FROM audit_events
			WHERE (created_at, id) < ($1, $2)
			ORDER BY created_at DESC, id DESC
			LIMIT $3
		`, params.AfterCreatedAt.UTC(), params.AfterID, limit+1)
	}
	if err != nil {
		return nil, storage.ListAuditEventsCursorParams{}, err
	}
	defer rows.Close()

	result := make([]storage.AuditEventRecord, 0, limit)
	for rows.Next() {
		var event storage.AuditEventRecord
		var detailsJSON []byte
		if err := rows.Scan(&event.ID, &event.ActorID, &event.Action, &event.TargetID, &detailsJSON, &event.CreatedAt, &event.PrevHash, &event.EventHash); err != nil {
			return nil, storage.ListAuditEventsCursorParams{}, err
		}
		event.CreatedAt = event.CreatedAt.UTC()
		if err := decodeJSON(detailsJSON, &event.Details); err != nil {
			return nil, storage.ListAuditEventsCursorParams{}, err
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, storage.ListAuditEventsCursorParams{}, err
	}

	var next storage.ListAuditEventsCursorParams
	if len(result) > limit {
		result = result[:limit]
		last := result[len(result)-1]
		next = storage.ListAuditEventsCursorParams{
			Limit:          limit,
			AfterCreatedAt: last.CreatedAt,
			AfterID:        last.ID,
		}
	}
	return result, next, nil
}

// PruneAuditEvents deletes audit_events rows with created_at strictly before
// the cutoff and returns the RowsAffected count (P2-REL-04 / finding M-R2).
// Relies on idx_audit_events_created_at (added in P2-DB-02) for efficiency.
//
// R-Q-03: routed through dbsqlc.PruneAuditEvents.
func (s *Store) PruneAuditEvents(ctx context.Context, before time.Time) (int64, error) {
	return dbsqlc.New(s.db).PruneAuditEvents(ctx, before.UTC())
}
