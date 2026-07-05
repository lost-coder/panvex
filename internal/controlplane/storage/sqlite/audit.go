package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) AppendAuditEvent(ctx context.Context, event storage.AuditEventRecord) error {
	detailsJSON, err := encodeJSON(event.Details)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO audit_events (id, actor_id, action, target_id, created_at_unix, details, prev_hash, event_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, event.ID, event.ActorID, event.Action, event.TargetID, toUnix(event.CreatedAt), detailsJSON, event.PrevHash, event.EventHash)
	return err
}

// AppendAuditEventsBulk inserts a batch of audit rows in one transaction
// (P6-6.1b). Same column set as AppendAuditEvent; `details` goes through
// encodeJSON per row. Chunked via runBulkChunks like every other bulk
// method in this package.
func (s *Store) AppendAuditEventsBulk(ctx context.Context, events []storage.AuditEventRecord) error {
	if len(events) == 0 {
		return nil
	}
	const cols = 8
	return s.execInTx(ctx, func(exec dbExecutor) error {
		return runBulkChunks(ctx, exec, len(events), cols,
			func(placeholders string) string {
				return fmt.Sprintf(`
					INSERT INTO audit_events (id, actor_id, action, target_id, created_at_unix, details, prev_hash, event_hash)
					VALUES %s`, placeholders)
			},
			func(start, end int) ([]any, error) {
				args := make([]any, 0, (end-start)*cols)
				for _, event := range events[start:end] {
					detailsJSON, err := encodeJSON(event.Details)
					if err != nil {
						return nil, err
					}
					args = append(args,
						event.ID, event.ActorID, event.Action, event.TargetID,
						toUnix(event.CreatedAt), detailsJSON, event.PrevHash, event.EventHash)
				}
				return args, nil
			},
		)
	})
}

// LatestAuditChainHash returns the EventHash of the most recently
// persisted audit row, or "" when the table is empty. Mirrors the
// postgres companion; producers rely on a stable empty string for the
// chain-genesis position.
func (s *Store) LatestAuditChainHash(ctx context.Context) (string, error) {
	var hash sql.NullString
	if err := s.db.QueryRowContext(ctx, `
		SELECT event_hash
		FROM audit_events
		ORDER BY created_at_unix DESC, id DESC
		LIMIT 1
	`).Scan(&hash); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return hash.String, nil
}

func (s *Store) ListAuditEvents(ctx context.Context, limit int) ([]storage.AuditEventRecord, error) {
	if limit <= 0 {
		limit = 1024
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_id, action, target_id, created_at_unix, details, prev_hash, event_hash
		FROM (SELECT id, actor_id, action, target_id, created_at_unix, details, prev_hash, event_hash
		      FROM audit_events ORDER BY created_at_unix DESC, id DESC LIMIT ?)
		ORDER BY created_at_unix, id
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.AuditEventRecord, 0)
	for rows.Next() {
		var event storage.AuditEventRecord
		var createdAt int64
		var detailsJSON string
		if err := rows.Scan(&event.ID, &event.ActorID, &event.Action, &event.TargetID, &createdAt, &detailsJSON, &event.PrevHash, &event.EventHash); err != nil {
			return nil, err
		}
		event.CreatedAt = fromUnix(createdAt)
		if err := decodeJSON(detailsJSON, &event.Details); err != nil {
			return nil, err
		}
		result = append(result, event)
	}

	return result, rows.Err()
}

// ListAuditEventsCursor returns one keyset-paginated page in (created_at
// DESC, id DESC) order — newest first, the operator's audit-page reading
// order. Contract is symmetric with ListJobsCursor: fetch limit+1 to detect
// "more", drop the overflow, and emit a next cursor pointing at the last
// in-page row.
func (s *Store) ListAuditEventsCursor(ctx context.Context, params storage.ListAuditEventsCursorParams) ([]storage.AuditEventRecord, storage.ListAuditEventsCursorParams, error) {
	limit := storage.NormalizeCursorLimit(params.Limit)

	var rows *sql.Rows
	var err error
	if params.AfterID == "" && params.AfterCreatedAt.IsZero() {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, actor_id, action, target_id, created_at_unix, details, prev_hash, event_hash
			FROM audit_events
			ORDER BY created_at_unix DESC, id DESC
			LIMIT ?
		`, limit+1)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, actor_id, action, target_id, created_at_unix, details, prev_hash, event_hash
			FROM audit_events
			WHERE (created_at_unix, id) < (?, ?)
			ORDER BY created_at_unix DESC, id DESC
			LIMIT ?
		`, toUnix(params.AfterCreatedAt), params.AfterID, limit+1)
	}
	if err != nil {
		return nil, storage.ListAuditEventsCursorParams{}, err
	}
	defer rows.Close()

	result := make([]storage.AuditEventRecord, 0, limit)
	for rows.Next() {
		var event storage.AuditEventRecord
		var createdAt int64
		var detailsJSON string
		if err := rows.Scan(&event.ID, &event.ActorID, &event.Action, &event.TargetID, &createdAt, &detailsJSON, &event.PrevHash, &event.EventHash); err != nil {
			return nil, storage.ListAuditEventsCursorParams{}, err
		}
		event.CreatedAt = fromUnix(createdAt)
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

// PruneAuditEvents deletes audit_events rows strictly older than before and
// returns the RowsAffected count. Exec-based to avoid pulling all rows through
// Go for retention worker efficiency (P2-REL-04).
func (s *Store) PruneAuditEvents(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM audit_events WHERE created_at_unix < ?`, toUnix(before))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
