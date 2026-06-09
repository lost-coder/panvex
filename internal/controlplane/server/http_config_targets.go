package server

import (
	"encoding/json"
	"errors"
	"net/http"

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

// groupConfigTargetResponse is the GET shape for a group scope: just the
// stored sections (empty object when no target exists).
type groupConfigTargetResponse struct {
	Sections map[string]any `json:"sections"`
}

// agentConfigTargetResponse is the GET shape for an agent scope: the
// agent's own override plus the group⊕override effective merge.
type agentConfigTargetResponse struct {
	Override  map[string]any `json:"override"`
	Effective map[string]any `json:"effective"`
}

// loadConfigTargetSections fetches the stored sections for one scope,
// returning an empty map when no target exists. A non-NotFound store
// error is propagated to the caller.
func (s *Server) loadConfigTargetSections(r *http.Request, scopeType, scopeID string) (map[string]any, error) {
	rec, err := s.store.GetAgentConfigTarget(r.Context(), scopeType, scopeID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	sections := map[string]any{}
	if rec.SectionsJSON != "" {
		if err := json.Unmarshal([]byte(rec.SectionsJSON), &sections); err != nil {
			return nil, err
		}
	}
	return sections, nil
}

// upsertConfigTarget validates the requested sections against the
// editable allowlist and persists them for the given scope, preserving
// the original CreatedAt across updates. Shared by the group and agent
// PUT handlers.
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
	encoded, err := json.Marshal(request.Sections)
	if err != nil {
		writeErrorLogged(r.Context(), w, http.StatusInternalServerError, msgInternalError, err)
		return
	}

	now := s.now()
	createdAt := now
	if existing, err := s.store.GetAgentConfigTarget(r.Context(), scopeType, scopeID); err == nil {
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, storage.ErrNotFound) {
		writeErrorLogged(r.Context(), w, http.StatusInternalServerError, msgConfigTargetReadFailed, err)
		return
	}

	if err := s.store.UpsertAgentConfigTarget(r.Context(), storage.AgentConfigTargetRecord{
		ScopeType:    scopeType,
		ScopeID:      scopeID,
		SectionsJSON: string(encoded),
		CreatedAt:    createdAt,
		UpdatedAt:    now,
	}); err != nil {
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
		sections, err := s.loadConfigTargetSections(r, storage.ConfigScopeGroup, id)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusInternalServerError, msgConfigTargetReadFailed, err)
			return
		}
		writeJSON(w, http.StatusOK, groupConfigTargetResponse{Sections: sections})
	}
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

// handleGetAgentConfigTarget returns the agent's own override and the
// group⊕override effective config. The agent's fleet group is read from
// the in-memory live snapshot; an agent with no group resolves an empty
// group config.
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
		groupID := existing.FleetGroupID
		groupSections := map[string]any{}
		if groupID != "" {
			var err error
			groupSections, err = s.loadConfigTargetSections(r, storage.ConfigScopeGroup, groupID)
			if err != nil {
				writeErrorLogged(r.Context(), w, http.StatusInternalServerError, msgConfigTargetReadFailed, err)
				return
			}
		}
		overrideSections, err := s.loadConfigTargetSections(r, storage.ConfigScopeAgent, id)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusInternalServerError, msgConfigTargetReadFailed, err)
			return
		}
		writeJSON(w, http.StatusOK, agentConfigTargetResponse{
			Override:  overrideSections,
			Effective: resolveEffectiveConfig(groupSections, overrideSections),
		})
	}
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
		s.upsertConfigTarget(w, r, storage.ConfigScopeAgent, id)
	}
}
