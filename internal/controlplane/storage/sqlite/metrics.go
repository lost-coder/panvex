package sqlite

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func (s *Store) AppendMetricSnapshot(ctx context.Context, snapshot storage.MetricSnapshotRecord) error {
	valuesJSON, err := encodeJSON(snapshot.Values)
	if err != nil {
		return err
	}

	// `values` is a reserved keyword in SQLite, so the identifier must be
	// double-quoted. The column was renamed from `values_json` in migration
	// 0011 (P2-DB-05 / DF-25) to match the Postgres schema.
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO metric_snapshots (id, agent_id, instance_id, captured_at_unix, "values")
		VALUES (?, ?, ?, ?, ?)
	`, snapshot.ID, snapshot.AgentID, snapshot.InstanceID, toUnix(snapshot.CapturedAt), valuesJSON)
	return err
}

func (s *Store) ListMetricSnapshots(ctx context.Context) ([]storage.MetricSnapshotRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, instance_id, captured_at_unix, "values"
		FROM (SELECT * FROM metric_snapshots ORDER BY captured_at_unix DESC, id DESC LIMIT 512)
		ORDER BY captured_at_unix, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.MetricSnapshotRecord, 0)
	for rows.Next() {
		var snapshot storage.MetricSnapshotRecord
		var capturedAt int64
		var valuesJSON string
		if err := rows.Scan(&snapshot.ID, &snapshot.AgentID, &snapshot.InstanceID, &capturedAt, &valuesJSON); err != nil {
			return nil, err
		}
		snapshot.CapturedAt = fromUnix(capturedAt)
		if err := decodeJSON(valuesJSON, &snapshot.Values); err != nil {
			return nil, err
		}
		result = append(result, snapshot)
	}

	return result, rows.Err()
}

// PruneMetricSnapshots deletes metric_snapshots rows strictly older than
// before and returns the RowsAffected count (P2-REL-05).
func (s *Store) PruneMetricSnapshots(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM metric_snapshots WHERE captured_at_unix < ?`, toUnix(before))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
