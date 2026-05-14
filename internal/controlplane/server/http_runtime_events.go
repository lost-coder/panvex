package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

const (
	// runtimeEventsDefaultLimit caps the default page size returned by
	// GET /api/agents/{id}/runtime-events when the caller does not pass
	// ?limit. Sized to comfortably fit one screen of the dashboard's
	// runtime-event panel while keeping the response JSON small.
	runtimeEventsDefaultLimit = 200
	// runtimeEventsMaxLimit clamps caller-supplied ?limit values so a
	// hostile or buggy client cannot request the entire per-agent ring
	// in one shot.
	runtimeEventsMaxLimit = 500
)

// handleListRuntimeEvents serves the read-side of the per-agent
// runtime-events ring buffer populated by handleRuntimeEventsBatch
// (gateway_messages.go). The endpoint is read-only and returns the
// newest events first; callers may filter by level (comma-separated
// list of slog-style levels) and cap the page size via ?limit.
//
// The handler returns an empty list for unknown agents rather than 404
// so the dashboard can poll a freshly-enrolled agent without
// special-casing the "no events yet" race window. s.runtimeEvents is
// constructed unconditionally in newServerFromOptions (lifecycle.go)
// but the nil-check stays as defence in depth.
func (s *Server) handleListRuntimeEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "agent id required")
			return
		}

		limit := runtimeEventsDefaultLimit
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				if n > runtimeEventsMaxLimit {
					n = runtimeEventsMaxLimit
				}
				limit = n
			}
		}

		var levels []string
		if v := r.URL.Query().Get("level"); v != "" {
			for _, lv := range strings.Split(v, ",") {
				lv = strings.TrimSpace(strings.ToLower(lv))
				if lv != "" {
					levels = append(levels, lv)
				}
			}
		}

		if s.runtimeEvents == nil {
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			return
		}

		evs := s.runtimeEvents.Snapshot(agentID, levels, limit)
		items := make([]map[string]any, 0, len(evs))
		for _, ev := range evs {
			items = append(items, map[string]any{
				"ts":      ev.Ts,
				"level":   ev.Level,
				"message": ev.Message,
				"fields":  ev.Fields,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}
