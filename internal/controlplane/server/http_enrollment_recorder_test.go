package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/dbsqlc"
	"github.com/lost-coder/panvex/internal/security"
)

// newEnrollmentRecorderTestServer wires a fresh SQLite store + Server pair
// so the inbound bootstrap handler runs against real persistence and the
// timeline recorder is engaged (the recorder is gated on stores that expose
// DB() — see lifecycle.go:initStoreBackedSubsystems). Returns the server,
// the *sqlite.Store (so negative tests can mutate token state via the
// storage API rather than raw SQL), and the underlying *sql.DB used by the
// dbsqlc-based assertion helpers.
func newEnrollmentRecorderTestServer(t *testing.T, now time.Time) (*Server, *sqlite.Store, *sql.DB) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})
	return server, store, store.DB()
}

func loadEnrollmentAttempt(t *testing.T, db *sql.DB, attemptID string) dbsqlc.EnrollmentAttempt {
	t.Helper()
	parsed, err := uuid.Parse(attemptID)
	if err != nil {
		t.Fatalf("parse attempt id %q: %v", attemptID, err)
	}
	row, err := dbsqlc.New(db).GetEnrollmentAttempt(context.Background(), parsed)
	if err != nil {
		t.Fatalf("GetEnrollmentAttempt(%s): %v", attemptID, err)
	}
	return row
}

// loadEnrollmentEventSteps reads only the step column for the attempt's
// events, ordered by ts then id. We intentionally do not go through
// dbsqlc.ListEnrollmentEvents here: that generated path scans
// fields_json into a plain json.RawMessage and panics on NULL — and the
// recorder inserts NULL when an event has no structured fields (e.g.
// the bootstrap_request_received event). The narrow projection keeps
// the test focused on event order, which is all we need to validate
// for the happy-path timeline.
func loadEnrollmentEventSteps(t *testing.T, db *sql.DB, attemptID string) []string {
	t.Helper()
	if _, err := uuid.Parse(attemptID); err != nil {
		t.Fatalf("parse attempt id %q: %v", attemptID, err)
	}
	rows, err := db.QueryContext(context.Background(),
		"SELECT step FROM enrollment_events WHERE attempt_id = ? ORDER BY ts ASC, id ASC",
		attemptID)
	if err != nil {
		t.Fatalf("query enrollment_events: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var step string
		if err := rows.Scan(&step); err != nil {
			t.Fatalf("scan enrollment_events.step: %v", err)
		}
		out = append(out, step)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate enrollment_events: %v", err)
	}
	return out
}

// lastEnrollmentAttemptID returns the most recent attempt row by started_at.
// Negative tests run against a fresh server so the ordering is unambiguous.
func lastEnrollmentAttemptID(t *testing.T, db *sql.DB) string {
	t.Helper()
	var raw string
	err := db.QueryRowContext(context.Background(),
		"SELECT id FROM enrollment_attempts ORDER BY started_at DESC, id DESC LIMIT 1").Scan(&raw)
	if err != nil {
		t.Fatalf("read last attempt: %v", err)
	}
	return raw
}

func attemptIDFromBootstrapBody(body []byte) string {
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ""
	}
	id, _ := decoded["attempt_id"].(string)
	return id
}

// sendBootstrapRaw bypasses json.Marshal so the test can ship an
// intentionally malformed body. Mirrors the request shape that
// performJSONRequestWithHeaders builds, minus the Origin/CSRF tweaks —
// /api/agent/bootstrap is exempt from CSRF middleware anyway.
func sendBootstrapRaw(t *testing.T, srv *Server, tokenValue string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/agent/bootstrap", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenValue)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestInboundEnrollmentRecordsHappyPath(t *testing.T) {
	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	srv, _, db := newEnrollmentRecorderTestServer(t, now)

	token, err := srv.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "default",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	resp := performJSONRequestWithHeaders(
		t,
		srv,
		http.MethodPost,
		"/api/agent/bootstrap",
		map[string]any{"node_name": "node-happy", "version": "0.0.0-test"},
		nil,
		map[string]string{
			"Authorization": "Bearer " + token.Value,
			"X-Request-Id":  "rid-happy",
		},
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if rid := resp.Header().Get("X-Request-Id"); rid != "rid-happy" {
		t.Fatalf("X-Request-Id echo = %q, want %q", rid, "rid-happy")
	}

	attemptID := attemptIDFromBootstrapBody(resp.Body.Bytes())
	if attemptID == "" {
		t.Fatalf("attempt_id missing from response body: %s", resp.Body.String())
	}

	steps := loadEnrollmentEventSteps(t, db, attemptID)
	want := []enrollment.Step{
		enrollment.StepBootstrapRequestReceived,
		enrollment.StepTokenValidated,
		enrollment.StepCertSigned,
		enrollment.StepCertReturned,
	}
	if len(steps) != len(want) {
		t.Fatalf("event count = %d, want %d (steps: %v)", len(steps), len(want), steps)
	}
	for i, expected := range want {
		if enrollment.Step(steps[i]) != expected {
			t.Errorf("event[%d].Step = %q, want %q", i, steps[i], expected)
		}
	}

	att := loadEnrollmentAttempt(t, db, attemptID)
	if att.Status != string(enrollment.StatusSuccess) {
		t.Errorf("attempt.Status = %q, want %q", att.Status, enrollment.StatusSuccess)
	}
	if att.RequestID != "rid-happy" {
		t.Errorf("attempt.RequestID = %q, want %q", att.RequestID, "rid-happy")
	}
	if att.Mode != string(enrollment.ModeInbound) {
		t.Errorf("attempt.Mode = %q, want %q", att.Mode, enrollment.ModeInbound)
	}
	if !att.FinishedAt.Valid {
		t.Errorf("attempt.FinishedAt should be set on success")
	}
	if !att.AgentID.Valid {
		t.Errorf("attempt.AgentID should be attached on success")
	}
}

func TestInboundEnrollmentRecordsMalformedBody(t *testing.T) {
	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	srv, _, db := newEnrollmentRecorderTestServer(t, now)

	token, err := srv.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "default",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	resp := sendBootstrapRaw(t, srv, token.Value, []byte("{not json"))
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body = %s)", resp.Code, http.StatusBadRequest, resp.Body.String())
	}

	attemptID := lastEnrollmentAttemptID(t, db)
	att := loadEnrollmentAttempt(t, db, attemptID)
	if !att.ErrorCode.Valid {
		t.Fatalf("attempt.ErrorCode is null (status=%q)", att.Status)
	}
	if got := enrollment.ErrorCode(att.ErrorCode.String); got != enrollment.ErrCSRInvalid {
		t.Errorf("attempt.ErrorCode = %q, want %q", got, enrollment.ErrCSRInvalid)
	}
	if att.Status != string(enrollment.StatusFailed) {
		t.Errorf("attempt.Status = %q, want %q", att.Status, enrollment.StatusFailed)
	}
}

func TestInboundEnrollmentRecordsExpiredToken(t *testing.T) {
	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	srv, store, db := newEnrollmentRecorderTestServer(t, now)

	token, err := srv.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "default",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	// Rewind expires_at to a point before "now" via the storage upsert. The
	// PutEnrollmentToken contract upserts on the value column so the existing
	// row is rewritten with the past expiry; subsequent
	// consumeEnrollmentTokenWithContext returns security.ErrEnrollmentTokenExpired.
	existing, err := store.GetEnrollmentToken(context.Background(), token.Value)
	if err != nil {
		t.Fatalf("GetEnrollmentToken() error = %v", err)
	}
	existing.ExpiresAt = now.Add(-time.Hour)
	if err := store.PutEnrollmentToken(context.Background(), existing); err != nil {
		t.Fatalf("PutEnrollmentToken() error = %v", err)
	}

	resp := performJSONRequestWithHeaders(
		t,
		srv,
		http.MethodPost,
		"/api/agent/bootstrap",
		map[string]any{"node_name": "node-expired", "version": "0.0.0-test"},
		nil,
		map[string]string{"Authorization": "Bearer " + token.Value},
	)
	if resp.Code == http.StatusOK {
		t.Fatalf("status = 200, want failure; body = %s", resp.Body.String())
	}

	attemptID := lastEnrollmentAttemptID(t, db)
	att := loadEnrollmentAttempt(t, db, attemptID)
	if !att.ErrorCode.Valid {
		t.Fatalf("attempt.ErrorCode is null (status=%q)", att.Status)
	}
	if got := enrollment.ErrorCode(att.ErrorCode.String); got != enrollment.ErrTokenExpired {
		t.Errorf("attempt.ErrorCode = %q, want %q", got, enrollment.ErrTokenExpired)
	}
}

func TestInboundEnrollmentRecordsUsedToken(t *testing.T) {
	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	srv, _, db := newEnrollmentRecorderTestServer(t, now)

	token, err := srv.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "default",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	first := performJSONRequestWithHeaders(
		t,
		srv,
		http.MethodPost,
		"/api/agent/bootstrap",
		map[string]any{"node_name": "node-first", "version": "0.0.0-test"},
		nil,
		map[string]string{"Authorization": "Bearer " + token.Value},
	)
	if first.Code != http.StatusOK {
		t.Fatalf("first call status = %d, body = %s", first.Code, first.Body.String())
	}

	second := performJSONRequestWithHeaders(
		t,
		srv,
		http.MethodPost,
		"/api/agent/bootstrap",
		map[string]any{"node_name": "node-second", "version": "0.0.0-test"},
		nil,
		map[string]string{"Authorization": "Bearer " + token.Value},
	)
	if second.Code == http.StatusOK {
		t.Fatalf("second call status = 200, want failure; body = %s", second.Body.String())
	}

	attemptID := lastEnrollmentAttemptID(t, db)
	att := loadEnrollmentAttempt(t, db, attemptID)
	if !att.ErrorCode.Valid {
		t.Fatalf("attempt.ErrorCode is null (status=%q)", att.Status)
	}
	if got := enrollment.ErrorCode(att.ErrorCode.String); got != enrollment.ErrTokenAlreadyUsed {
		t.Errorf("attempt.ErrorCode = %q, want %q", got, enrollment.ErrTokenAlreadyUsed)
	}
}
