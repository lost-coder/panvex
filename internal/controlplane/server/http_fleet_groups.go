package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/fleet"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const (
	msgFleetGroupIDReq    = "fleet group id is required"
	msgFleetGroupNotFound = "fleet group not found"
)

// fleetGroupResponse is the JSON shape returned by list and detail
// endpoints. agent_count is computed from the live in-memory agent
// snapshot (not persisted), matching the legacy /fleet-groups
// contract that the frontend already consumes.
type fleetGroupResponse struct {
	ID            string                          `json:"id"`
	Name          string                          `json:"name"`
	Label         string                          `json:"label"`
	Description   string                          `json:"description"`
	AgentCount    int                             `json:"agent_count"`
	CreatedAtUnix int64                           `json:"created_at_unix"`
	UpdatedAtUnix int64                           `json:"updated_at_unix"`
	Integrations  []fleetGroupIntegrationResponse `json:"integrations"`
}

type fleetGroupIntegrationResponse struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	ProviderID    string          `json:"provider_id,omitempty"`
	Enabled       bool            `json:"enabled"`
	Config        json.RawMessage `json:"config"`
	CreatedAtUnix int64           `json:"created_at_unix"`
	UpdatedAtUnix int64           `json:"updated_at_unix"`
}

type createFleetGroupRequest struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type updateFleetGroupRequest struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type fleetGroupDeletionPreviewResponse struct {
	ID                    string `json:"id"`
	AgentCount            int64  `json:"agent_count"`
	EnrollmentTokenCount  int64  `json:"enrollment_token_count"`
	ClientAssignmentCount int64  `json:"client_assignment_count"`
	ReassignRequired      bool   `json:"reassign_required"`
}

type fleetGroupDeletionResponse struct {
	Moved fleetGroupDeletionPreviewResponse `json:"moved"`
}

func (s *Server) handleListFleetGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		// R-S-14: only return groups inside the operator's scope.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		groups, err := s.fleetSvc.List(r.Context())
		if err != nil {
			s.logger.ErrorContext(r.Context(), "list fleet groups failed", "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		response := make([]fleetGroupResponse, 0, len(groups))
		for _, g := range groups {
			if !scope.IsAllowed(g.ID) {
				continue
			}
			response = append(response, s.fleetGroupToResponse(r.Context(), g, false))
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func (s *Server) handleGetFleetGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgFleetGroupIDReq)
			return
		}
		// R-S-14: scope-check the fleet-group id before any read so a
		// non-admin operator outside of this group's scope receives 404
		// (not 403) — leaking "this group exists, you just can't see it"
		// is itself an information disclosure for an IDOR probe.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(id) {
			writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
			return
		}
		group, err := s.fleetSvc.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
				return
			}
			s.logger.ErrorContext(r.Context(), "get fleet group failed", "id", id, "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		writeJSON(w, http.StatusOK, s.fleetGroupToResponse(r.Context(), group, true))
	}
}

func (s *Server) handleCreateFleetGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var request createFleetGroupRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid fleet group payload")
			return
		}
		group, err := s.fleetSvc.Create(r.Context(), fleet.CreateInput{
			Name:        request.Name,
			Label:       request.Label,
			Description: request.Description,
		})
		if err != nil {
			s.writeFleetGroupError(r.Context(), w, err)
			return
		}
		s.appendAuditWithContext(r.Context(), session.UserID, "fleet_groups.create", group.ID, map[string]any{
			"name":  group.Name,
			"label": group.Label,
		})
		writeJSON(w, http.StatusCreated, s.fleetGroupToResponse(r.Context(), group, true))
	}
}

func (s *Server) handleUpdateFleetGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgFleetGroupIDReq)
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
		var request updateFleetGroupRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid fleet group payload")
			return
		}
		group, err := s.fleetSvc.Update(r.Context(), id, fleet.UpdateInput{
			Label:       request.Label,
			Description: request.Description,
		})
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
				return
			}
			s.writeFleetGroupError(r.Context(), w, err)
			return
		}
		s.appendAuditWithContext(r.Context(), session.UserID, "fleet_groups.update", group.ID, map[string]any{
			"label":       group.Label,
			"description": group.Description,
		})
		writeJSON(w, http.StatusOK, s.fleetGroupToResponse(r.Context(), group, true))
	}
}

func (s *Server) handleFleetGroupDeletionPreview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgFleetGroupIDReq)
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(id) {
			writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
			return
		}
		counts, err := s.fleetSvc.DeletionPreview(r.Context(), id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
				return
			}
			s.logger.ErrorContext(r.Context(), "fleet group deletion preview failed", "id", id, "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		writeJSON(w, http.StatusOK, fleetGroupDeletionPreviewResponse{
			ID:                    id,
			AgentCount:            counts.Agents,
			EnrollmentTokenCount:  counts.EnrollmentTokens,
			ClientAssignmentCount: counts.ClientAssignments,
			ReassignRequired:      counts.Agents+counts.EnrollmentTokens+counts.ClientAssignments > 0,
		})
	}
}

func (s *Server) handleDeleteFleetGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgFleetGroupIDReq)
			return
		}
		reassignTo, ok := s.authorizeFleetGroupDelete(w, r, user, id)
		if !ok {
			return
		}
		moved, err := s.fleetSvc.Delete(r.Context(), id, reassignTo)
		if err != nil {
			s.writeFleetGroupDeleteError(r.Context(), w, id, err)
			return
		}
		s.appendAuditWithContext(r.Context(), session.UserID, "fleet_groups.delete", id, map[string]any{
			"reassign_to":              reassignTo,
			"agents_moved":             moved.Agents,
			"enrollment_tokens_moved":  moved.EnrollmentTokens,
			"client_assignments_moved": moved.ClientAssignments,
		})
		// After reassigning agents.fleet_group_id in the DB the in-memory
		// Agent snapshot still points at the deleted group id. Patch the
		// cached copies so subsequent /fleet-groups queries report the
		// correct membership without waiting for a heartbeat rewrite.
		if moved.Agents > 0 {
			s.patchAgentFleetGroupMembership(id, reassignTo)
		}
		writeJSON(w, http.StatusOK, fleetGroupDeletionResponse{
			Moved: fleetGroupDeletionPreviewResponse{
				ID:                    id,
				AgentCount:            moved.Agents,
				EnrollmentTokenCount:  moved.EnrollmentTokens,
				ClientAssignmentCount: moved.ClientAssignments,
				ReassignRequired:      false,
			},
		})
	}
}

// authorizeFleetGroupDelete validates the operator scope (R-S-14) for both
// the group being deleted and the reassign target. Returns the validated
// reassign-target id and ok=true on success; writes the appropriate HTTP
// error and returns ok=false otherwise.
func (s *Server) authorizeFleetGroupDelete(w http.ResponseWriter, r *http.Request, user auth.User, id string) (string, bool) {
	scope, ok := s.requireFleetScope(w, r, user)
	if !ok {
		return "", false
	}
	if !scope.IsAllowed(id) {
		writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
		return "", false
	}
	reassignTo := r.URL.Query().Get("reassign_to")
	if reassignTo != "" && !scope.IsAllowed(reassignTo) {
		writeError(w, http.StatusBadRequest, "reassign target outside operator scope")
		return "", false
	}
	return reassignTo, true
}

// writeFleetGroupDeleteError maps service-level errors from fleetSvc.Delete
// to HTTP responses. Pulled out so the handler stays linear.
func (s *Server) writeFleetGroupDeleteError(ctx context.Context, w http.ResponseWriter, id string, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
	case errors.Is(err, fleet.ErrReassignTargetMissing):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, fleet.ErrReassignTargetSame):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.logger.ErrorContext(ctx, "delete fleet group failed", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
	}
}

// patchAgentFleetGroupMembership rewrites the in-memory agent snapshot so
// agents previously pointing at deletedID now reference reassignTo.
func (s *Server) patchAgentFleetGroupMembership(deletedID, reassignTo string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Identity-only update across the affected agents. Taken under s.mu (s.mu ->
	// live) so the membership rewrite is atomic w.r.t. other s.mu holders.
	for _, agent := range s.live.List() {
		if agent.FleetGroupID == deletedID {
			s.updateAgentIdentity(agent.ID, func(a *Agent) {
				a.FleetGroupID = reassignTo
			})
		}
	}
}

// fleetGroupToResponse assembles the JSON representation. When
// withIntegrations is true we also fetch the per-group integrations
// list (used by the detail endpoint). The agent_count is read from
// the server's in-memory agent snapshot so it matches the live
// /agents endpoint — persistence lags heartbeats by a batch interval
// and would show stale membership.
func (s *Server) fleetGroupToResponse(ctx context.Context, g storage.FleetGroupRecord, withIntegrations bool) fleetGroupResponse {
	agentCount := 0
	for _, agent := range s.live.List() {
		if agent.FleetGroupID == g.ID {
			agentCount++
		}
	}

	response := fleetGroupResponse{
		ID:            g.ID,
		Name:          g.Name,
		Label:         g.Label,
		Description:   g.Description,
		AgentCount:    agentCount,
		CreatedAtUnix: g.CreatedAt.UTC().Unix(),
		UpdatedAtUnix: g.UpdatedAt.UTC().Unix(),
		Integrations:  []fleetGroupIntegrationResponse{},
	}

	if !withIntegrations {
		return response
	}
	integrations, err := s.store.ListFleetGroupIntegrations(ctx, g.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "list fleet group integrations failed", "fleet_group_id", g.ID, "error", err)
		return response
	}
	// Deterministic order: kind first, then created_at. Storage layer
	// already returns that order but the empty-slice fallback above
	// means we need to materialise here too.
	sort.SliceStable(integrations, func(i, j int) bool {
		if integrations[i].Kind != integrations[j].Kind {
			return integrations[i].Kind < integrations[j].Kind
		}
		return integrations[i].CreatedAt.Before(integrations[j].CreatedAt)
	})
	for _, i := range integrations {
		providerID := ""
		if i.ProviderID != nil {
			providerID = *i.ProviderID
		}
		config := json.RawMessage(i.Config)
		if len(config) == 0 {
			config = json.RawMessage("{}")
		}
		response.Integrations = append(response.Integrations, fleetGroupIntegrationResponse{
			ID:            i.ID,
			Kind:          i.Kind,
			ProviderID:    providerID,
			Enabled:       i.Enabled,
			Config:        config,
			CreatedAtUnix: i.CreatedAt.UTC().Unix(),
			UpdatedAtUnix: i.UpdatedAt.UTC().Unix(),
		})
	}
	return response
}

// writeFleetGroupError maps domain-level validation errors to HTTP
// status codes. Unknown errors fall through to 500 — the handler
// logs the underlying cause.
func (s *Server) writeFleetGroupError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fleet.ErrNameRequired),
		errors.Is(err, fleet.ErrNameInvalid),
		errors.Is(err, fleet.ErrNameTooLong),
		errors.Is(err, fleet.ErrLabelRequired),
		errors.Is(err, fleet.ErrLabelTooLong),
		errors.Is(err, fleet.ErrDescriptionTooLong):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, fleet.ErrNameInUse):
		writeError(w, http.StatusConflict, err.Error())
	default:
		s.logger.ErrorContext(ctx, "fleet group mutation failed", "error", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
	}
}
