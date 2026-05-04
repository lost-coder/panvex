package server

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// agentVisibleInScope reports whether the operator's scope grants
// access to the given agent's fleet group. A non-existent agent is
// treated as visible so callers preserve their existing 404 path —
// hiding "exists but out-of-scope" behind the same response keeps
// cross-scope probes from learning agent ids.
func (s *Server) agentVisibleInScope(scope FleetScopeAccess, agentID string) bool {
	if scope.Global {
		return true
	}
	s.mu.RLock()
	agent, ok := s.agents[agentID]
	s.mu.RUnlock()
	if !ok {
		return true
	}
	return scope.IsAllowed(agent.FleetGroupID)
}

const (
	defaultHistoryRangeHours = 24
	maxHistoryRangeHours     = 24 * 90
)

func (s *Server) handleServerLoadHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}

		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing server id")
			return
		}
		if !s.agentVisibleInScope(scope, agentID) {
			writeError(w, http.StatusNotFound, msgServerNotFound)
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
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}

		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing server id")
			return
		}
		if !s.agentVisibleInScope(scope, agentID) {
			writeError(w, http.StatusNotFound, msgServerNotFound)
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
//
// CountryCode/CountryName/City/ASN are populated by the GeoIP Manager
// when one is loaded (see Server.geoip). They are emitted with
// `omitempty` so panels running with GeoIP disabled — or rows whose IP
// has no DB record (private/loopback/unknown) — see the legacy shape
// {ip_address, first_seen, last_seen} unchanged.
type clientIPRow struct {
	IPAddress   string    `json:"ip_address"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	CountryCode string    `json:"country_code,omitempty"`
	CountryName string    `json:"country_name,omitempty"`
	City        string    `json:"city,omitempty"`
	ASN         string    `json:"asn,omitempty"`
}

// enrichIPRows fills in the GeoIP-derived fields on each row when a
// Manager is loaded. Extracted from handleClientIPHistory so the
// enrichment logic is unit-testable without seeding client_ip_history.
// Skips unparseable IPs (defensive — AggregateClientIPHistory only
// returns rows that originally went through net.IP serialisation, but
// a future caller might not). Private/loopback addresses are filtered
// inside Manager.LookupCity/LookupASN via ShouldLookup, so the loop
// itself does not need to repeat that policy.
func (s *Server) enrichIPRows(rows []clientIPRow) {
	if s.geoip == nil {
		return
	}
	for i := range rows {
		ip := net.ParseIP(rows[i].IPAddress)
		if ip == nil {
			continue
		}
		if city, ok := s.geoip.LookupCity(ip); ok {
			rows[i].CountryCode = city.CountryCode
			rows[i].CountryName = city.CountryName
			rows[i].City = city.City
		}
		if asn, ok := s.geoip.LookupASN(ip); ok {
			rows[i].ASN = asn.Display()
		}
	}
}

func (s *Server) handleClientIPHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, "missing client id")
			return
		}
		if !scope.Global {
			_, assignments, _, lookupErr := s.clientDetailSnapshot(clientID)
			if lookupErr != nil {
				if errors.Is(lookupErr, storage.ErrNotFound) {
					writeError(w, http.StatusNotFound, msgClientNotFound)
					return
				}
				s.logger.Error("client ip history scope lookup failed", "client_id", clientID, "error", lookupErr)
				writeError(w, http.StatusInternalServerError, msgInternalError)
				return
			}
			if !s.clientInScope(scope, assignments) {
				writeError(w, http.StatusNotFound, msgClientNotFound)
				return
			}
		}

		from, to := s.parseTimeRange(r, 24*30) // default 30 days for IPs
		if s.store == nil {
			writeJSON(w, http.StatusOK, map[string]any{"ips": []any{}, "total_unique": 0})
			return
		}

		// Q4.U-P-04 follow-up: push the per-IP fold into SQL via
		// AggregateClientIPHistory + LIMIT. Pull (limit + 1) so we can
		// detect truncation without a separate COUNT round-trip;
		// total_unique reflects the raw distinct count and uses the
		// dedicated CountUniqueClientIPs query.
		limit := parseClientIPHistoryLimit(r)
		aggregates, err := s.store.AggregateClientIPHistory(r.Context(), clientID, from, to, limit+1)
		if err != nil {
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		truncated := false
		if len(aggregates) > limit {
			aggregates = aggregates[:limit]
			truncated = true
		}
		ips := make([]clientIPRow, len(aggregates))
		for i, agg := range aggregates {
			ips[i] = clientIPRow{
				IPAddress: agg.IPAddress,
				FirstSeen: agg.FirstSeen,
				LastSeen:  agg.LastSeen,
			}
		}
		s.enrichIPRows(ips)
		totalUnique, err := s.store.CountUniqueClientIPs(r.Context(), clientID)
		if err != nil {
			s.logger.Warn("count unique client ips failed", "client_id", clientID, "error", err)
			totalUnique = len(ips)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ips":          ips,
			"total_unique": totalUnique,
			"truncated":    truncated,
			"limit":        limit,
		})
	}
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
