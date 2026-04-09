package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	defaultHistoryRangeHours = 24
	maxHistoryRangeHours     = 24 * 90
)

func (s *Server) handleServerLoadHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing server id")
			return
		}

		from, to := parseTimeRange(r, defaultHistoryRangeHours)
		if s.store == nil {
			writeJSON(w, http.StatusOK, map[string]any{"points": []any{}, "resolution": "raw"})
			return
		}

		retention := s.retentionSettings()
		rawCutoff := s.now().UTC().Add(-time.Duration(retention.TSRawSeconds) * time.Second)

		// If requested range starts within raw retention, use raw points.
		if from.After(rawCutoff) || from.Equal(rawCutoff) {
			points, err := s.store.ListServerLoadPoints(r.Context(), agentID, from, to)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"points": points, "resolution": "raw"})
			return
		}

		// Otherwise fall back to hourly rollups.
		points, err := s.store.ListServerLoadHourly(r.Context(), agentID, from, to)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"points": points, "resolution": "hourly"})
	}
}

func (s *Server) handleDCHealthHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing server id")
			return
		}

		from, to := parseTimeRange(r, defaultHistoryRangeHours)
		if s.store == nil {
			writeJSON(w, http.StatusOK, map[string]any{"points": []any{}})
			return
		}

		points, err := s.store.ListDCHealthPoints(r.Context(), agentID, from, to)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"points": points})
	}
}

func (s *Server) handleClientIPHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, "missing client id")
			return
		}

		from, to := parseTimeRange(r, 24*30) // default 30 days for IPs
		if s.store == nil {
			writeJSON(w, http.StatusOK, map[string]any{"ips": []any{}, "total_unique": 0})
			return
		}

		records, err := s.store.ListClientIPHistory(r.Context(), clientID, from, to)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ips": records, "total_unique": len(records)})
	}
}

func parseTimeRange(r *http.Request, defaultHours int) (time.Time, time.Time) {
	now := time.Now().UTC()
	to := now
	from := now.Add(-time.Duration(defaultHours) * time.Hour)

	if v := r.URL.Query().Get("from"); v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			from = parsed.UTC()
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			to = parsed.UTC()
		}
	}

	// Clamp range
	if to.Sub(from) > time.Duration(maxHistoryRangeHours)*time.Hour {
		from = to.Add(-time.Duration(maxHistoryRangeHours) * time.Hour)
	}

	return from, to
}
