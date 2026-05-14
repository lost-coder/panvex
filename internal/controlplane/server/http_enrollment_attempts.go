package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
)

// defaultEnrollmentAttemptsLimit and maxEnrollmentAttemptsLimit cap the
// list endpoint so a panel session can't accidentally pull thousands of
// rows over a slow link. The dashboard never asks for more than a page at
// a time; operators can paginate by repeating the call with a tighter
// filter once cursor support arrives in Phase 2.
const (
	defaultEnrollmentAttemptsLimit = 20
	maxEnrollmentAttemptsLimit     = 100
)

// handleListEnrollmentAttempts exposes recent enrollment attempts to the
// dashboard. The endpoint is read-only and lives behind the existing
// authenticated session middleware. Returns 503 when the recorder is not
// wired (a test fixture without a *sql.DB store) so the panel UI can
// degrade gracefully instead of erroring.
func (s *Server) handleListEnrollmentAttempts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.enrollmentRec == nil {
			writeError(w, http.StatusServiceUnavailable, "enrollment recorder not available")
			return
		}
		ctx := r.Context()
		q := r.URL.Query()
		f := enrollment.ListFilter{Limit: defaultEnrollmentAttemptsLimit}
		if v := q.Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				if n > maxEnrollmentAttemptsLimit {
					n = maxEnrollmentAttemptsLimit
				}
				f.Limit = n
			}
		}
		if v := q.Get("token_id"); v != "" {
			f.TokenID = &v
		}
		if v := q.Get("agent_id"); v != "" {
			f.AgentID = &v
		}
		if v := q.Get("status"); v != "" {
			st := enrollment.Status(v)
			f.Status = &st
		}
		if v := q.Get("mode"); v != "" {
			md := enrollment.Mode(v)
			f.Mode = &md
		}
		if v := q.Get("error_code"); v != "" {
			f.ErrorCode = &v
		}
		if v := q.Get("started_after"); v != "" {
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				f.StartedAfter = &ts
			}
		}
		if v := q.Get("started_before"); v != "" {
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				f.StartedBefore = &ts
			}
		}
		if v := q.Get("cursor"); v != "" {
			if c, err := decodeAttemptCursor(v); err == nil {
				f.CursorTs = &c.Ts
				f.CursorID = &c.ID
			}
		}

		page, err := s.enrollmentRec.ListAttemptsPage(ctx, f)
		if err != nil {
			writeErrorLogged(ctx, w, http.StatusInternalServerError, "list enrollment attempts", err)
			return
		}
		resp := map[string]any{
			"items":       page.Items,
			"next_cursor": nil,
		}
		if page.NextCursor != nil {
			resp["next_cursor"] = encodeAttemptCursor(*page.NextCursor)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// encodeAttemptCursor packs an AttemptCursor into a URL-safe base64
// payload. The cursor is opaque to the client; the panel UI is
// expected to round-trip it without inspection.
func encodeAttemptCursor(c enrollment.AttemptCursor) string {
	body := map[string]any{"ts": c.Ts.UTC().Format(time.RFC3339Nano), "id": c.ID}
	b, _ := json.Marshal(body)
	return base64.URLEncoding.EncodeToString(b)
}

// decodeAttemptCursor reverses encodeAttemptCursor. A malformed cursor
// returns an error; the handler treats that as "no cursor" so a stale
// or tampered query string returns the first page rather than a 4xx.
func decodeAttemptCursor(s string) (enrollment.AttemptCursor, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return enrollment.AttemptCursor{}, err
	}
	var m struct {
		Ts string `json:"ts"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return enrollment.AttemptCursor{}, err
	}
	ts, err := time.Parse(time.RFC3339Nano, m.Ts)
	if err != nil {
		return enrollment.AttemptCursor{}, err
	}
	return enrollment.AttemptCursor{Ts: ts, ID: m.ID}, nil
}

// handleGetEnrollmentAttempt returns a single attempt and its complete
// timeline. Missing attempts surface as 404 so the dashboard can show a
// dedicated "this attempt was retained-out" message instead of a generic
// error toast.
func (s *Server) handleGetEnrollmentAttempt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.enrollmentRec == nil {
			writeError(w, http.StatusServiceUnavailable, "enrollment recorder not available")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		out, err := s.enrollmentRec.GetAttemptWithEvents(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "get enrollment attempt")
			return
		}
		if out == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}
