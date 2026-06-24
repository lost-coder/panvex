package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type discoveredClientConflict struct {
	Type       string   `json:"type"`
	RelatedIDs []string `json:"related_ids"`
}

type discoveredClientResponse struct {
	ID                 string                     `json:"id"`
	AgentID            string                     `json:"agent_id"`
	NodeName           string                     `json:"node_name"`
	ClientName         string                     `json:"client_name"`
	Status             string                     `json:"status"`
	TotalOctets        uint64                     `json:"total_octets"`
	CurrentConnections int                        `json:"current_connections"`
	ActiveUniqueIPs    int                        `json:"active_unique_ips"`
	ConnectionLinks    []string                   `json:"connection_links"`
	MaxTCPConns        int                        `json:"max_tcp_conns"`
	MaxUniqueIPs       int                        `json:"max_unique_ips"`
	DataQuotaBytes     int64                      `json:"data_quota_bytes"`
	Expiration         string                     `json:"expiration"`
	DiscoveredAt       int64                      `json:"discovered_at_unix"`
	UpdatedAt          int64                      `json:"updated_at_unix"`
	Conflicts          []discoveredClientConflict `json:"conflicts,omitempty"`
}

func (s *Server) handleDiscoveredClients() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		clients, err := s.listDiscoveredClients(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		clients = s.filterDiscoveredClientsByScope(clients, scope)

		sortDiscoveredClientsByName(clients)
		conflicts := detectDiscoveredClientConflicts(clients)
		agentNodeNames := s.resolveAgentNodeNames(clients)

		response := make([]discoveredClientResponse, 0, len(clients))
		for _, dc := range clients {
			resp := discoveredClientToResponse(dc)
			resp.NodeName = agentNodeNames[dc.AgentID]
			if c, ok := conflicts[dc.ID]; ok {
				resp.Conflicts = c
			}
			response = append(response, resp)
		}

		writeJSON(w, http.StatusOK, response)
	}
}

// filterDiscoveredClientsByScope drops discovered clients whose parent
// agent sits outside the operator's scope. R-S-14: the parent agent's
// fleet_group_id is the scope key; an unknown parent (race with
// deregister) is dropped because we cannot scope-check it safely.
// Global scope short-circuits the lock acquisition.
func (s *Server) filterDiscoveredClientsByScope(clients []discoveredClient, scope FleetScopeAccess) []discoveredClient {
	if scope.Global {
		return clients
	}
	filtered := clients[:0]
	for _, dc := range clients {
		agent, agentOK := s.live.Get(dc.AgentID)
		if !agentOK || !scope.IsAllowed(agent.FleetGroupID) {
			continue
		}
		filtered = append(filtered, dc)
	}
	return filtered
}

// discoveredClientInScope reports whether the given discovered-client
// id resolves to an agent inside the operator's scope. Used by
// adopt/ignore. Returns (true, nil) when scope is global.
func (s *Server) discoveredClientInScope(ctx context.Context, scope FleetScopeAccess, dcID string) (bool, error) {
	if scope.Global {
		return true, nil
	}

	if s.discoveredRepo == nil {
		return false, nil
	}
	rec, err := s.discoveredRepo.Get(ctx, discovered.DiscoveredID(dcID))
	if err != nil {
		return false, err
	}
	agent, agentOK := s.live.Get(rec.AgentID)
	if !agentOK {
		return false, nil
	}
	return scope.IsAllowed(agent.FleetGroupID), nil
}

func (s *Server) handleAdoptDiscoveredClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		id := chi.URLParam(r, "id")
		if !s.checkDiscoveredClientScope(w, r, scope, id) {
			return
		}
		client, err := s.adoptDiscoveredClient(r.Context(), id, session.UserID, s.now())
		if err != nil {
			writeAdoptDiscoveredClientError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"client_id": string(client.ID),
			"name":      client.Name,
		})
	}
}

// checkDiscoveredClientScope writes the appropriate HTTP response when
// the discovered client is missing or out of scope, returning false in
// that case. R-S-14.
func (s *Server) checkDiscoveredClientScope(w http.ResponseWriter, r *http.Request, scope FleetScopeAccess, id string) bool {
	inScope, scopeErr := s.discoveredClientInScope(r.Context(), scope, id)
	if scopeErr != nil {
		if errors.Is(scopeErr, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
			return false
		}
		s.logger.Error("scope-check discovered client failed", "id", id, "error", scopeErr)
		writeError(w, http.StatusInternalServerError, msgInternalError)
		return false
	}
	if !inScope {
		writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
		return false
	}
	return true
}

// writeAdoptDiscoveredClientError maps an adopt failure to the matching
// HTTP response. Mirrors the original three-branch switch.
func writeAdoptDiscoveredClientError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
	case errors.Is(err, ErrAlreadyAdopted):
		writeError(w, http.StatusConflict, err.Error())
	default:
		handleClientMutationError(w, err)
	}
}

type bulkAdoptRequest struct {
	IDs []string `json:"ids"`
}

type bulkAdoptResponse struct {
	Results             []BulkAdoptResult `json:"results"`
	AdoptedCount        int               `json:"adopted_count"`
	AlreadyAdoptedCount int               `json:"already_adopted_count"`
	ErrorCount          int               `json:"error_count"`
	SkippedOutOfScope   int               `json:"skipped_out_of_scope,omitempty"`
}

// maxBulkAdoptIDs caps a single bulk-adopt request. The 1 MiB request
// body limit is the real ceiling — at ~25 bytes per ID that is roughly
// 40k ids — but adoptMu is held for the whole batch and SQLite is a
// single writer, so we still want a sane upper bound. 10k is enough
// for any realistic fleet without forcing the operator to chunk.
const maxBulkAdoptIDs = 10_000

// handleBulkAdoptDiscoveredClients adopts every discovered id in one
// HTTP request. Each id is scope-checked individually; out-of-scope ids
// are dropped (not surfaced as errors so an operator with partial scope
// cannot probe ids belonging to other groups). The whole batch shares
// one rate-limit token so legitimate fleet-wide adopts don't tarball
// against the per-user sensitive limiter.
func (s *Server) handleBulkAdoptDiscoveredClients() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		var req bulkAdoptRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if len(req.IDs) == 0 {
			writeJSON(w, http.StatusOK, bulkAdoptResponse{Results: []BulkAdoptResult{}})
			return
		}
		if len(req.IDs) > maxBulkAdoptIDs {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("too many ids (max %d)", maxBulkAdoptIDs))
			return
		}

		filtered, skipped, err := s.filterBulkAdoptIDsInScope(r.Context(), req.IDs, scope)
		if err != nil {
			s.logger.Error("scope-check discovered client failed (bulk)", "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}

		results := s.bulkAdoptDiscoveredClients(r.Context(), filtered, session.UserID, s.now())

		resp := bulkAdoptResponse{Results: results, SkippedOutOfScope: skipped}
		resp.AdoptedCount, resp.AlreadyAdoptedCount, resp.ErrorCount = countBulkAdoptResults(results)
		writeJSON(w, http.StatusOK, resp)
	}
}

// filterBulkAdoptIDsInScope dedupes the requested ids and drops the ones the
// caller's scope can't reach, before the adopt write-lock is taken. Ids that
// no longer exist (ErrNotFound) and out-of-scope ids are counted as skipped;
// any other scope-check failure aborts with a wrapped error.
func (s *Server) filterBulkAdoptIDsInScope(ctx context.Context, ids []string, scope FleetScopeAccess) (filtered []string, skipped int, err error) {
	seen := make(map[string]struct{}, len(ids))
	filtered = make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		inScope, scopeErr := s.discoveredClientInScope(ctx, scope, id)
		if scopeErr != nil {
			if errors.Is(scopeErr, storage.ErrNotFound) {
				skipped++
				continue
			}
			return nil, 0, fmt.Errorf("discovered client %q: %w", id, scopeErr)
		}
		if !inScope {
			skipped++
			continue
		}
		filtered = append(filtered, id)
	}
	return filtered, skipped, nil
}

// countBulkAdoptResults tallies a bulk-adopt result set into its per-status
// counters (adopted / already-adopted / everything else as an error).
func countBulkAdoptResults(results []BulkAdoptResult) (adopted, alreadyAdopted, errorCount int) {
	for _, r := range results {
		switch r.Status {
		case "adopted":
			adopted++
		case "already_adopted":
			alreadyAdopted++
		default:
			errorCount++
		}
	}
	return adopted, alreadyAdopted, errorCount
}

func (s *Server) handleIgnoreDiscoveredClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		id := chi.URLParam(r, "id")
		inScope, scopeErr := s.discoveredClientInScope(r.Context(), scope, id)
		if scopeErr != nil {
			if errors.Is(scopeErr, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
				return
			}
			s.logger.Error("scope-check discovered client failed", "id", id, "error", scopeErr)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		if !inScope {
			writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
			return
		}
		if err := s.ignoreDiscoveredClient(r.Context(), id, session.UserID, s.now()); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
			} else {
				writeError(w, http.StatusInternalServerError, msgInternalError)
			}
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func discoveredClientToResponse(dc discoveredClient) discoveredClientResponse {
	links := dc.ConnectionLinks
	if links == nil {
		links = []string{}
	}
	return discoveredClientResponse{
		ID:                 dc.ID,
		AgentID:            dc.AgentID,
		ClientName:         dc.ClientName,
		Status:             dc.Status,
		TotalOctets:        dc.TotalOctets,
		CurrentConnections: dc.CurrentConnections,
		ActiveUniqueIPs:    dc.ActiveUniqueIPs,
		ConnectionLinks:    links,
		MaxTCPConns:        dc.MaxTCPConns,
		MaxUniqueIPs:       dc.MaxUniqueIPs,
		DataQuotaBytes:     dc.DataQuotaBytes,
		Expiration:         dc.Expiration,
		DiscoveredAt:       dc.DiscoveredAt.Unix(),
		UpdatedAt:          dc.UpdatedAt.Unix(),
	}
}

// resolveAgentNodeNames maps agent IDs → node names from the agents cache.
func (s *Server) resolveAgentNodeNames(clients []discoveredClient) map[string]string {
	result := make(map[string]string)
	for _, dc := range clients {
		if _, ok := result[dc.AgentID]; ok {
			continue
		}
		if agent, ok := s.live.Get(dc.AgentID); ok {
			result[dc.AgentID] = agent.NodeName
		}
	}
	return result
}

// discoveredConflictGroup groups discovered-client IDs that share a
// pivot key (secret or name) and tracks the distinct opposite-axis
// values so the caller can decide whether the group is a real conflict.
type discoveredConflictGroup struct {
	ids       []string
	otherAxis map[string]struct{}
}

// otherIDs returns every id in the group except `id`.
func (g *discoveredConflictGroup) otherIDs(id string) []string {
	others := make([]string, 0, len(g.ids)-1)
	for _, oid := range g.ids {
		if oid != id {
			others = append(others, oid)
		}
	}
	return others
}

// recordDiscoveredConflicts walks groups and emits conflict entries
// of the given kind for every id in any group with more than one
// distinct value on the opposite axis.
func recordDiscoveredConflicts(groups map[string]*discoveredConflictGroup, kind string, result map[string][]discoveredClientConflict) {
	for _, g := range groups {
		if len(g.otherAxis) <= 1 {
			continue
		}
		for _, id := range g.ids {
			result[id] = append(result[id], discoveredClientConflict{
				Type:       kind,
				RelatedIDs: g.otherIDs(id),
			})
		}
	}
}

// detectDiscoveredClientConflicts identifies conflicts among discovered clients:
// - same_secret_different_names: multiple names share one secret
// - same_name_different_secrets: one name has multiple different secrets
func detectDiscoveredClientConflicts(clients []discoveredClient) map[string][]discoveredClientConflict {
	bySecret := make(map[string]*discoveredConflictGroup)
	byName := make(map[string]*discoveredConflictGroup)

	for _, dc := range clients {
		if dc.Secret != "" {
			g, ok := bySecret[dc.Secret]
			if !ok {
				g = &discoveredConflictGroup{otherAxis: make(map[string]struct{})}
				bySecret[dc.Secret] = g
			}
			g.ids = append(g.ids, dc.ID)
			g.otherAxis[dc.ClientName] = struct{}{}
		}

		g, ok := byName[dc.ClientName]
		if !ok {
			g = &discoveredConflictGroup{otherAxis: make(map[string]struct{})}
			byName[dc.ClientName] = g
		}
		g.ids = append(g.ids, dc.ID)
		if dc.Secret != "" {
			g.otherAxis[dc.Secret] = struct{}{}
		}
	}

	result := make(map[string][]discoveredClientConflict)
	recordDiscoveredConflicts(bySecret, "same_secret_different_names", result)
	recordDiscoveredConflicts(byName, "same_name_different_secrets", result)
	return result
}

// handleRescanDiscoveredClients asks every currently-connected agent to
// re-report its full Telemt client list, so operators can force a discovery
// refresh (e.g. after fixing a Telemt outage on a node) without waiting for the
// periodic cycle or restarting the agent. Returns 202 with the number of agents
// nudged; the actual re-discovery happens asynchronously on each agent's stream.
func (s *Server) handleRescanDiscoveredClients() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := s.sessions.RequestRediscoveryAll()
		s.logger.InfoContext(r.Context(), "operator-triggered client re-discovery", "agents_notified", n)
		writeJSON(w, http.StatusAccepted, map[string]int{"agents_notified": n})
	}
}
