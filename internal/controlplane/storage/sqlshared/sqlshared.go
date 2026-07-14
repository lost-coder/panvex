// Package sqlshared holds the pieces of the SQLite and PostgreSQL stores that
// are genuinely dialect-independent. It is deliberately small: anything that
// differs between the two backends — placeholder syntax, JSON column types,
// the executor surface — stays in its own dialect package, because pretending
// otherwise is how a "shared" helper acquires an `if postgres` branch.
package sqlshared

import "database/sql"

// BulkChunkSize caps how many rows go into a single multi-row INSERT. Both
// backends use the same bound: it keeps the statement's placeholder count
// well inside PostgreSQL's 65535-parameter limit and SQLite's variable limit,
// while still collapsing a 10k-row batch into a handful of round-trips.
const BulkChunkSize = 250

// ChunkBounds returns [start, end) for the next chunk of at most size rows
// that fits within total.
func ChunkBounds(start, total, size int) (int, int) {
	end := start + size
	if end > total {
		end = total
	}
	return start, end
}

// NullString maps Go's empty string to SQL NULL. Both stores need this because
// the schema distinguishes "no value" from "empty value" on nullable columns.
func NullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
