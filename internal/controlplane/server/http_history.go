package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
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

		from, to := s.parseTimeRange(r, defaultHistoryRangeHours)
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
				writeError(w, http.StatusInternalServerError, msgInternalError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"points": points, "resolution": "raw"})
			return
		}

		// Otherwise fall back to hourly rollups.
		points, err := s.store.ListServerLoadHourly(r.Context(), agentID, from, to)
		if err != nil {
			writeError(w, http.StatusInternalServerError, msgInternalError)
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

		from, to := s.parseTimeRange(r, defaultHistoryRangeHours)
		if s.store == nil {
			writeJSON(w, http.StatusOK, map[string]any{"points": []any{}})
			return
		}

		points, err := s.store.ListDCHealthPoints(r.Context(), agentID, from, to)
		if err != nil {
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"points": points})
	}
}

// clientIPRow is the public shape returned by handleClientIPHistory.
// First/last seen are aggregated across nodes so the same physical IP
// only shows up once per client even when several agents report it.
type clientIPRow struct {
	IPAddress string    `json:"ip_address"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
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

		from, to := s.parseTimeRange(r, 24*30) // default 30 days for IPs
		if s.store == nil {
			writeJSON(w, http.StatusOK, map[string]any{"ips": []any{}, "total_unique": 0})
			return
		}

		records, err := s.store.ListClientIPHistory(r.Context(), clientID, from, to)
		if err != nil {
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}

		ips := collapseIPHistoryByIP(records)
		totalUnique := len(ips)
		limit := parseClientIPHistoryLimit(r)
		truncated := false
		if len(ips) > limit {
			ips = ips[:limit]
			truncated = true
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ips":          ips,
			"total_unique": totalUnique,
			"truncated":    truncated,
			"limit":        limit,
		})
	}
}

// collapseIPHistoryByIP folds the per-(agent, client, ip) rows into one
// row per IP — first_seen is the earliest sighting across nodes,
// last_seen the most recent — and sorts by most-recently-seen first.
func collapseIPHistoryByIP(records []storage.ClientIPHistoryRecord) []clientIPRow {
	byIP := make(map[string]*clientIPRow, len(records))
	for _, rec := range records {
		row, ok := byIP[rec.IPAddress]
		if !ok {
			byIP[rec.IPAddress] = &clientIPRow{
				IPAddress: rec.IPAddress,
				FirstSeen: rec.FirstSeen,
				LastSeen:  rec.LastSeen,
			}
			continue
		}
		if rec.FirstSeen.Before(row.FirstSeen) {
			row.FirstSeen = rec.FirstSeen
		}
		if rec.LastSeen.After(row.LastSeen) {
			row.LastSeen = rec.LastSeen
		}
	}
	ips := make([]clientIPRow, 0, len(byIP))
	for _, row := range byIP {
		ips = append(ips, *row)
	}
	sort.Slice(ips, func(i, j int) bool { return ips[i].LastSeen.After(ips[j].LastSeen) })
	return ips
}

// parseClientIPHistoryLimit honours the operator's ?limit= override
// while clamping to defaultClientIPHistoryMax (Q4.U-P-04: a high-
// cardinality client can otherwise produce a multi-megabyte payload).
func parseClientIPHistoryLimit(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return defaultClientIPHistoryLimit
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return defaultClientIPHistoryLimit
	}
	if parsed > defaultClientIPHistoryMax {
		return defaultClientIPHistoryMax
	}
	return parsed
}

// Q4.U-P-04: top-N defaults for the per-client IP history endpoint.
const (
	defaultClientIPHistoryLimit = 200
	defaultClientIPHistoryMax   = 2000
)

// parseTimeRange now takes an explicit now so it stays deterministic
// under the injectable clock (Q5.U-Q-16). Call sites pass s.now().
func (s *Server) parseTimeRange(r *http.Request, defaultHours int) (time.Time, time.Time) {
	return parseTimeRangeAt(r, defaultHours, s.now())
}

func parseTimeRangeAt(r *http.Request, defaultHours int, nowAt time.Time) (time.Time, time.Time) {
	now := nowAt.UTC()
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
