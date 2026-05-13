package dbsqlc_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/dbsqlc"
	_ "modernc.org/sqlite"
)

// TestListEnrollmentEventsScansNull guards the fix that made the
// enrollment_events.fields_json column scan as *json.RawMessage so a
// NULL value scans into a nil pointer instead of panicking inside
// encoding/json.RawMessage.Scan. The recorder writes NULL when an
// event has no structured fields, and Task 21 (HTTP API) reads back
// the timeline via ListEnrollmentEvents, so a NULL row must round-trip
// cleanly.
func TestListEnrollmentEventsScansNull(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.ExecContext(context.Background(), `
        CREATE TABLE enrollment_attempts (id TEXT PRIMARY KEY);
        CREATE TABLE enrollment_events (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            attempt_id TEXT NOT NULL,
            ts TIMESTAMP NOT NULL,
            step TEXT NOT NULL,
            level TEXT NOT NULL,
            message TEXT,
            fields_json TEXT
        );
    `)
	if err != nil {
		t.Fatal(err)
	}
	attempt := uuid.New()
	_, err = db.ExecContext(context.Background(), `INSERT INTO enrollment_attempts (id) VALUES (?)`, attempt.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO enrollment_events (attempt_id, ts, step, level, message, fields_json) VALUES (?, datetime('now'), 'step1', 'info', NULL, NULL)`, attempt.String())
	if err != nil {
		t.Fatal(err)
	}

	rows, err := dbsqlc.New(db).ListEnrollmentEvents(context.Background(), attempt)
	if err != nil {
		t.Fatalf("ListEnrollmentEvents: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].FieldsJson != nil {
		t.Fatalf("FieldsJson = %v, want nil", rows[0].FieldsJson)
	}
}
