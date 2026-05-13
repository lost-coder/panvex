package server

import (
	"net/http"
	"strconv"

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
		f := enrollment.ListFilter{Limit: defaultEnrollmentAttemptsLimit}
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				if n > maxEnrollmentAttemptsLimit {
					n = maxEnrollmentAttemptsLimit
				}
				f.Limit = n
			}
		}
		if v := r.URL.Query().Get("token_id"); v != "" {
			f.TokenID = &v
		}
		if v := r.URL.Query().Get("agent_id"); v != "" {
			f.AgentID = &v
		}
		if v := r.URL.Query().Get("status"); v != "" {
			st := enrollment.Status(v)
			f.Status = &st
		}
		out, err := s.enrollmentRec.ListAttempts(r.Context(), f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list enrollment attempts")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": out})
	}
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
