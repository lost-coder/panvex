package postgres

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

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO metric_snapshots (id, agent_id, instance_id, captured_at, values)
		VALUES ($1, $2, $3, $4, $5)
	`, snapshot.ID, snapshot.AgentID, snapshot.InstanceID, snapshot.CapturedAt.UTC(), valuesJSON)
	return err
}

func (s *Store) ListMetricSnapshots(ctx context.Context) ([]storage.MetricSnapshotRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, instance_id, captured_at, values
		FROM (SELECT * FROM metric_snapshots ORDER BY captured_at DESC, id DESC LIMIT 512) sub
		ORDER BY captured_at, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]storage.MetricSnapshotRecord, 0)
	for rows.Next() {
		var snapshot storage.MetricSnapshotRecord
		var valuesJSON []byte
		if err := rows.Scan(&snapshot.ID, &snapshot.AgentID, &snapshot.InstanceID, &snapshot.CapturedAt, &valuesJSON); err != nil {
			return nil, err
		}
		snapshot.CapturedAt = snapshot.CapturedAt.UTC()
		if err := decodeJSON(valuesJSON, &snapshot.Values); err != nil {
			return nil, err
		}
		result = append(result, snapshot)
	}

	return result, rows.Err()
}

// PruneMetricSnapshots deletes metric_snapshots rows with captured_at strictly
// before the cutoff and returns the RowsAffected count (P2-REL-05). Relies on
// idx_metric_snapshots_captured_at (added in P2-DB-02) for efficiency.
func (s *Store) PruneMetricSnapshots(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM metric_snapshots WHERE captured_at < $1`, before.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
