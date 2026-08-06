package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const (
	msgConfigTargetIDReq      = "scope id is required"
	msgConfigTargetInvalid    = "invalid config target payload"
	msgConfigTargetReadFailed = "failed to read config target"
)

// configTargetRequest is the PUT body for both group and agent scopes.
// Sections is a sparse map of editable top-level Telemt config sections.
type configTargetRequest struct {
	Sections map[string]any `json:"sections"`
}

// groupConfigTargetResponse is the GET shape for a group scope: the stored
// sections (empty object when no target exists) plus a per-agent drift
// summary for the group's in-scope agents.
type groupConfigTargetResponse struct {
	Sections map[string]any         `json:"sections"`
	Nodes    []groupConfigNodeDrift `json:"nodes"`
}

// agentConfigTargetResponse is the GET shape for an agent scope: the
// agent's own desired snapshot (the full stored config, stripped of the P2
// schema-version marker), the group⊕desired effective merge (used to mark
// locked/group-owned fields in the UI), the node's observed editable
// sections, the drift of observed vs desired, and the dotted paths the
// node's fleet group governs (GroupPaths — P4 Task 5) so the UI can lock
// those fields without re-deriving them from the effective merge.
type agentConfigTargetResponse struct {
	Desired    map[string]any  `json:"desired"`
	Effective  map[string]any  `json:"effective"`
	Observed   map[string]any  `json:"observed"`
	Drift      configDriftView `json:"drift"`
	GroupPaths []string        `json:"group_paths"`
}

// configDriftView is the drift summary attached to a config GET response.
// Status is one of "in_sync", "drifted", "unknown"; Fields is the list of
// dotted paths that drift (empty unless Status == "drifted").
type configDriftView struct {
	Status string   `json:"status"`
	Fields []string `json:"fields"`
}

// groupConfigNodeDrift summarizes one agent's drift status for the group
// GET response.
type groupConfigNodeDrift struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
}

// observedConfigForAgent returns the parsed observed editable sections for an
// agent (from the live store) and whether any observed data exists.
func (s *Server) observedConfigForAgent(agentID string) (map[string]any, bool) {
	for _, inst := range s.live.InstancesForAgent(agentID) {
		if inst.ManagedConfigJSON == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(inst.ManagedConfigJSON), &m); err == nil {
			return m, true
		}
	}
	return map[string]any{}, false
}

// driftView computes the drift view of observed vs target. target is the
// effective (group⊕override) config for the group GET response, or the
// desired (stripped agent snapshot) config for the agent GET response.
func driftView(target, observed map[string]any, hasObserved bool) configDriftView {
	if !hasObserved {
		return configDriftView{Status: "unknown", Fields: []string{}}
	}
	drifted, fields := configDrift(target, observed)
	if fields == nil {
		fields = []string{}
	}
	status := "in_sync"
	if drifted {
		status = "drifted"
	}
	return configDriftView{Status: status, Fields: fields}
}

// upsertConfigTarget validates the requested sections against the editable
// allowlist and REPLACES the stored sections for the given scope through the
// configtargets service (which preserves the original CreatedAt across
// updates). Used by the group PUT handler only — the group scope has no P2
// full-snapshot semantics to preserve, unlike the agent scope, which merges
// via upsertAgentConfigTarget instead.
func (s *Server) upsertConfigTarget(w http.ResponseWriter, r *http.Request, scopeType, scopeID string) {
	var request configTargetRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, msgConfigTargetInvalid)
		return
	}
	if request.Sections == nil {
		request.Sections = map[string]any{}
	}
	if err := validateEditableSections(request.Sections); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateNoProcessOwnedFields(request.Sections); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.configTargets.Upsert(r.Context(), scopeType, scopeID, request.Sections); err != nil {
		writeErrorLogged(r.Context(), w, http.StatusInternalServerError, msgInternalError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// upsertAgentConfigTarget validates the requested sparse sections and MERGES
// them into the agent's stored full-snapshot config target (P2 Task 5): the
// incoming sections' leaves win field-by-field (nested maps merge
// recursively via deepMergeConfigInto), but any path NOT present in the
// request — including the __schema_version marker seedDesiredConfig stamps
// — is preserved from the existing snapshot. Without this an operator PUTing
// a single field (e.g. general.log_level) would silently wipe every other
// field seeded from the agent's own observed config, which is exactly the
// full-snapshot regression P2 exists to prevent. Unlike the group scope
// (upsertConfigTarget), an agent target always exists once the agent has
// reported once (seedDesiredConfig), so "no existing snapshot" only happens
// for an agent that has never reported — that case degrades gracefully to a
// plain create.
func (s *Server) upsertAgentConfigTarget(w http.ResponseWriter, r *http.Request, agentID string) {
	var request configTargetRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, msgConfigTargetInvalid)
		return
	}
	if request.Sections == nil {
		request.Sections = map[string]any{}
	}
	if err := validateEditableSections(request.Sections); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateNoProcessOwnedFields(request.Sections); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	existing, err := s.configTargets.Sections(r.Context(), storage.ConfigScopeAgent, agentID)
	if err != nil {
		writeErrorLogged(r.Context(), w, http.StatusInternalServerError, msgInternalError, err)
		return
	}
	merged := deepCopyConfigMap(existing)
	if merged == nil {
		merged = map[string]any{}
	}
	deepMergeConfigInto(merged, request.Sections)
	if err := s.configTargets.Upsert(r.Context(), storage.ConfigScopeAgent, agentID, merged); err != nil {
		writeErrorLogged(r.Context(), w, http.StatusInternalServerError, msgInternalError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleGetGroupConfigTarget returns the stored sections for a fleet
// group scope. Missing target → {"sections": {}}.
func (s *Server) handleGetGroupConfigTarget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgConfigTargetIDReq)
			return
		}
		// R-S-14: scope-check the fleet-group id before any read so an
		// out-of-scope operator receives the same not-found response the
		// sibling /fleet-groups/{id} endpoints return (no information
		// disclosure about a group's existence).
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(id) {
			writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
			return
		}
		sections, err := s.configTargets.Sections(r.Context(), storage.ConfigScopeGroup, id)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusInternalServerError, msgConfigTargetReadFailed, err)
			return
		}
		nodes, err := s.groupConfigNodeDrifts(r, id, sections)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusInternalServerError, msgConfigTargetReadFailed, err)
			return
		}
		writeJSON(w, http.StatusOK, groupConfigTargetResponse{Sections: sections, Nodes: nodes})
	}
}

// groupConfigNodeDrifts computes the per-agent drift rows for a fleet group:
// for every live agent in the group, resolve its effective config (group ⊕
// override, override stripped of the P2 schema-version marker) against the
// last observed snapshot. Any override-load failure is surfaced so the
// handler can return 500.
func (s *Server) groupConfigNodeDrifts(r *http.Request, groupID string, groupSections map[string]any) ([]groupConfigNodeDrift, error) {
	nodes := []groupConfigNodeDrift{}
	for _, agent := range s.live.List() {
		if agent.FleetGroupID != groupID {
			continue
		}
		override, err := s.configTargets.Sections(r.Context(), storage.ConfigScopeAgent, agent.ID)
		if err != nil {
			return nil, err
		}
		effective := resolveEffectiveConfig(groupSections, strippedSnapshot(override))
		observed, hasObserved := s.observedConfigForAgent(agent.ID)
		nodes = append(nodes, groupConfigNodeDrift{
			AgentID: agent.ID,
			Status:  driftView(effective, observed, hasObserved).Status,
		})
	}
	return nodes, nil
}

// handlePutGroupConfigTarget validates and upserts the config target for
// a fleet group scope.
func (s *Server) handlePutGroupConfigTarget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgConfigTargetIDReq)
			return
		}
		// R-S-14: writes are gated on scope to mirror reads.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(id) {
			writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
			return
		}
		s.upsertConfigTarget(w, r, storage.ConfigScopeGroup, id)
	}
}

// handleGetAgentConfigTarget returns the agent's own desired snapshot and the
// group⊕desired effective config. The agent's fleet group is read from
// the in-memory live snapshot; an agent with no group resolves an empty
// group config. Drift is computed desired-vs-observed (P2): the effective
// merge is retained only so the UI can mark group-locked fields.
func (s *Server) handleGetAgentConfigTarget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgConfigTargetIDReq)
			return
		}
		// R-S-14: the agent must exist in the live snapshot and the
		// operator's fleet scope must cover its group before any read.
		// Out-of-scope (or unknown) agents get the same not-found
		// response the sibling /agents/{id} endpoints return.
		existing, exists := s.live.Get(id)
		if !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(existing.FleetGroupID) {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}
		desired, effective, groupPaths, err := s.loadAgentEffectiveConfig(r.Context(), id, existing.FleetGroupID)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusInternalServerError, msgConfigTargetReadFailed, err)
			return
		}
		observed, hasObserved := s.observedConfigForAgent(id)
		writeJSON(w, http.StatusOK, agentConfigTargetResponse{
			Desired:    desired,
			Effective:  effective,
			Observed:   observed,
			Drift:      driftView(desired, observed, hasObserved),
			GroupPaths: groupPaths,
		})
	}
}

// loadAgentEffectiveConfig loads an agent's own desired snapshot (stripped of
// the P2 schema-version marker), the effective config (its fleet group's
// sections ⊕ the desired snapshot), and the flattened dotted paths the
// group's sections govern (P4 Task 5 — used by the UI to lock those fields).
// An empty groupID resolves an empty group config and empty group paths.
func (s *Server) loadAgentEffectiveConfig(ctx context.Context, agentID, groupID string) (desired, effective map[string]any, groupPaths []string, err error) {
	groupSections := map[string]any{}
	if groupID != "" {
		groupSections, err = s.configTargets.Sections(ctx, storage.ConfigScopeGroup, groupID)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	raw, err := s.configTargets.Sections(ctx, storage.ConfigScopeAgent, agentID)
	if err != nil {
		return nil, nil, nil, err
	}
	desired = strippedSnapshot(raw)
	return desired, resolveEffectiveConfig(groupSections, desired), flattenConfigPaths(groupSections), nil
}

// flattenConfigPaths flattens a nested config sections map into a sorted,
// non-nil list of dotted leaf paths (e.g. {"censorship":{"tls_domain":"x"}}
// -> ["censorship.tls_domain"]). Used to report which paths a fleet group's
// config target governs (GroupPaths), so the UI can lock those fields.
func flattenConfigPaths(m map[string]any) []string {
	paths := []string{}
	var walk func(prefix string, node map[string]any)
	walk = func(prefix string, node map[string]any) {
		for k, v := range node {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			if nested, ok := v.(map[string]any); ok {
				walk(path, nested)
				continue
			}
			paths = append(paths, path)
		}
	}
	walk("", m)
	sort.Strings(paths)
	return paths
}

// handlePutAgentConfigTarget validates and upserts the config override
// for an agent scope.
func (s *Server) handlePutAgentConfigTarget() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgConfigTargetIDReq)
			return
		}
		// R-S-14: writes are gated on scope to mirror reads — the agent
		// must exist and its fleet group must be in the operator's scope.
		existing, exists := s.live.Get(id)
		if !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(existing.FleetGroupID) {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}
		s.upsertAgentConfigTarget(w, r, id)
	}
}
