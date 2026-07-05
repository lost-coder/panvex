package sqlite

import (
	"context"
	"fmt"
	"time"
)

// pruneChunkSize bounds a single retention DELETE so a catch-up prune
// after long downtime never holds the SQLite writer lock for an unbounded
// interval (P6-6.3d, finding #14 — mirrors postgres/timeseries.go's
// ctid-chunked pattern via rowid; no table in the schema is WITHOUT ROWID).
// A var, not a const: tests shrink it to exercise the multi-chunk loop
// without inserting 10k+ rows.
var pruneChunkSize = 10_000

// pruneMaxIterations caps the number of chunks one Prune call will burn
// through. With chunk=10k that is up to 50M rows per invocation before
// yielding to the next retention tick — same figure as the Postgres side.
const pruneMaxIterations = 5_000

// pruneChunked deletes rows of `table` whose `tsColumn` is strictly below
// cutoffUnix, at most pruneChunkSize rows per DELETE statement, and
// returns the total rows removed. Each statement is a short writer-lock
// acquisition, so the batch writer interleaves between chunks.
func (s *Store) pruneChunked(ctx context.Context, table, tsColumn string, cutoff time.Time) (int64, error) {
	//nolint:gosec // table and tsColumn are compile-time constants supplied by the Prune* wrappers below, never user input
	query := fmt.Sprintf(
		`DELETE FROM %s WHERE rowid IN (SELECT rowid FROM %s WHERE %s < ? LIMIT ?)`,
		table, table, tsColumn)
	cutoffUnix := toUnix(cutoff)
	var total int64
	for i := 0; i < pruneMaxIterations; i++ {
		result, err := s.db.ExecContext(ctx, query, cutoffUnix, pruneChunkSize)
		if err != nil {
			return total, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += affected
		if affected < int64(pruneChunkSize) {
			return total, nil
		}
	}
	return total, nil
}
