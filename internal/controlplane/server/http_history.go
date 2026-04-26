package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
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
		// client_ip_history is keyed by (agent_id, client_id, ip_address),
		// so one physical IP seen on N nodes shows up as N rows. Collapse
		// here by IP — first_seen = earliest sighting across nodes,
		// last_seen = most recent — so the card shows each IP once.
		type ipRow struct {
			IPAddress string    `json:"ip_address"`
			FirstSeen time.Time `json:"first_seen"`
			LastSeen  time.Time `json:"last_seen"`
		}
		byIP := make(map[string]*ipRow, len(records))
		for _, r := range records {
			row, ok := byIP[r.IPAddress]
			if !ok {
				byIP[r.IPAddress] = &ipRow{
					IPAddress: r.IPAddress,
					FirstSeen: r.FirstSeen,
					LastSeen:  r.LastSeen,
				}
				continue
			}
			if r.FirstSeen.Before(row.FirstSeen) {
				row.FirstSeen = r.FirstSeen
			}
			if r.LastSeen.After(row.LastSeen) {
				row.LastSeen = r.LastSeen
			}
		}
		ips := make([]ipRow, 0, len(byIP))
		for _, row := range byIP {
			ips = append(ips, *row)
		}
		sort.Slice(ips, func(i, j int) bool { return ips[i].LastSeen.After(ips[j].LastSeen) })
		// Q4.U-P-04: cap the response. A high-cardinality client (open
		// proxy / botnet target) can otherwise produce a multi-megabyte
		// payload. ?limit= overrides up to defaultClientIPHistoryMax.
		totalUnique := len(ips)
		limit := defaultClientIPHistoryLimit
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				if parsed > defaultClientIPHistoryMax {
					parsed = defaultClientIPHistoryMax
				}
				limit = parsed
			}
		}
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

// Q4.U-P-04: top-N defaults for the per-client IP history endpoint.
const (
	defaultClientIPHistoryLimit = 200
	defaultClientIPHistoryMax   = 2000
)

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
