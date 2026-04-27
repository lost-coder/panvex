package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/fleet"
	"github.com/lost-coder/panvex/internal/controlplane/fleet/integrations"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// ---- Kind catalogs ---------------------------------------------------

type integrationKindResponse struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ProviderKind string `json:"provider_kind,omitempty"`
}

type providerKindResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleListIntegrationKinds() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		kinds := s.fleetSvc.IntegrationRegistry().List()
		response := make([]integrationKindResponse, 0, len(kinds))
		for _, k := range kinds {
			response = append(response, integrationKindResponse{
				Name:         k.Name(),
				Description:  k.Description(),
				ProviderKind: k.ProviderKind(),
			})
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func (s *Server) handleListProviderKinds() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		kinds := s.fleetSvc.ProviderRegistry().List()
		response := make([]providerKindResponse, 0, len(kinds))
		for _, k := range kinds {
			response = append(response, providerKindResponse{
				Name:        k.Name(),
				Description: k.Description(),
			})
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// ---- Providers --------------------------------------------------------

type integrationProviderResponse struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	Label         string          `json:"label"`
	Config        json.RawMessage `json:"config"`
	CreatedAtUnix int64           `json:"created_at_unix"`
	UpdatedAtUnix int64           `json:"updated_at_unix"`
}

type createIntegrationProviderRequest struct {
	Kind   string          `json:"kind"`
	Label  string          `json:"label"`
	Config json.RawMessage `json:"config"`
}

type updateIntegrationProviderRequest struct {
	Label  string          `json:"label"`
	Config json.RawMessage `json:"config"`
}

func providerToResponse(p storage.IntegrationProviderRecord) integrationProviderResponse {
	config := json.RawMessage(p.Config)
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	return integrationProviderResponse{
		ID:            p.ID,
		Kind:          p.Kind,
		Label:         p.Label,
		Config:        config,
		CreatedAtUnix: p.CreatedAt.UTC().Unix(),
		UpdatedAtUnix: p.UpdatedAt.UTC().Unix(),
	}
}

func (s *Server) handleListIntegrationProviders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		records, err := s.fleetSvc.ListProviders(r.Context())
		if err != nil {
			s.logger.Error("list integration providers failed", "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		response := make([]integrationProviderResponse, 0, len(records))
		for _, p := range records {
			response = append(response, providerToResponse(p))
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func (s *Server) handleGetIntegrationProvider() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgProviderIDRequired)
			return
		}
		provider, err := s.fleetSvc.GetProvider(r.Context(), id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgProviderNotFound)
				return
			}
			s.logger.Error("get integration provider failed", "id", id, "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		writeJSON(w, http.StatusOK, providerToResponse(provider))
	}
}

func (s *Server) handleCreateIntegrationProvider() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var request createIntegrationProviderRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid provider payload")
			return
		}
		provider, err := s.fleetSvc.CreateProvider(r.Context(), fleet.CreateProviderInput{
			Kind:   request.Kind,
			Label:  request.Label,
			Config: request.Config,
		})
		if err != nil {
			s.writeIntegrationError(w, err)
			return
		}
		s.appendAuditWithContext(r.Context(), session.UserID, "integration_providers.create", provider.ID, map[string]any{
			"kind":  provider.Kind,
			"label": provider.Label,
		})
		writeJSON(w, http.StatusCreated, providerToResponse(provider))
	}
}

func (s *Server) handleUpdateIntegrationProvider() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgProviderIDRequired)
			return
		}
		var request updateIntegrationProviderRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid provider payload")
			return
		}
		provider, err := s.fleetSvc.UpdateProvider(r.Context(), id, fleet.UpdateProviderInput{
			Label:  request.Label,
			Config: request.Config,
		})
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgProviderNotFound)
				return
			}
			s.writeIntegrationError(w, err)
			return
		}
		s.appendAuditWithContext(r.Context(), session.UserID, "integration_providers.update", provider.ID, map[string]any{
			"label": provider.Label,
		})
		writeJSON(w, http.StatusOK, providerToResponse(provider))
	}
}

func (s *Server) handleDeleteIntegrationProvider() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgProviderIDRequired)
			return
		}
		if err := s.fleetSvc.DeleteProvider(r.Context(), id); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgProviderNotFound)
				return
			}
			s.logger.Error("delete integration provider failed", "id", id, "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		s.appendAuditWithContext(r.Context(), session.UserID, "integration_providers.delete", id, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---- Per-group integrations ------------------------------------------

type installFleetGroupIntegrationRequest struct {
	Kind       string          `json:"kind"`
	ProviderID *string         `json:"provider_id,omitempty"`
	Enabled    bool            `json:"enabled"`
	Config     json.RawMessage `json:"config"`
}

type updateFleetGroupIntegrationRequest struct {
	ProviderID *string         `json:"provider_id,omitempty"`
	Enabled    bool            `json:"enabled"`
	Config     json.RawMessage `json:"config"`
}

func (s *Server) handleInstallFleetGroupIntegration() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		groupID := chi.URLParam(r, "id")
		if groupID == "" {
			writeError(w, http.StatusBadRequest, "fleet group id is required")
			return
		}
		// R-S-14: integration writes follow the parent group's scope.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(groupID) {
			writeError(w, http.StatusNotFound, "fleet group not found")
			return
		}
		var request installFleetGroupIntegrationRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid integration payload")
			return
		}
		record, err := s.fleetSvc.InstallIntegration(r.Context(), groupID, fleet.InstallIntegrationInput{
			Kind:       request.Kind,
			ProviderID: request.ProviderID,
			Config:     request.Config,
			Enabled:    request.Enabled,
		})
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "fleet group or provider not found")
				return
			}
			s.writeIntegrationError(w, err)
			return
		}
		s.appendAuditWithContext(r.Context(), session.UserID, "fleet_group_integrations.install", record.ID, map[string]any{
			"fleet_group_id": record.FleetGroupID,
			"kind":           record.Kind,
		})
		writeJSON(w, http.StatusCreated, fleetGroupIntegrationRecordToResponse(record))
	}
}

func (s *Server) handleGetFleetGroupIntegration() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "integrationId")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgIntegrationIDRequired)
			return
		}
		record, err := s.fleetSvc.GetIntegration(r.Context(), id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgIntegrationNotFound)
				return
			}
			s.logger.Error("get integration failed", "id", id, "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		// R-S-14: scope-check by parent group.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(record.FleetGroupID) {
			writeError(w, http.StatusNotFound, msgIntegrationNotFound)
			return
		}
		writeJSON(w, http.StatusOK, fleetGroupIntegrationRecordToResponse(record))
	}
}

// loadIntegrationForScopedMutation looks up the integration record
// and verifies the operator's scope owns its fleet group. Returns
// (record, true) on success; on failure it writes the right HTTP
// error and returns (zero, false).
func (s *Server) loadIntegrationForScopedMutation(w http.ResponseWriter, r *http.Request, user auth.User, id, getErrLog string) (storage.FleetGroupIntegrationRecord, bool) {
	existing, err := s.fleetSvc.GetIntegration(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, msgIntegrationNotFound)
			return storage.FleetGroupIntegrationRecord{}, false
		}
		s.logger.Error(getErrLog, "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
		return storage.FleetGroupIntegrationRecord{}, false
	}
	scope, ok := s.requireFleetScope(w, r, user)
	if !ok {
		return storage.FleetGroupIntegrationRecord{}, false
	}
	if !scope.IsAllowed(existing.FleetGroupID) {
		writeError(w, http.StatusNotFound, msgIntegrationNotFound)
		return storage.FleetGroupIntegrationRecord{}, false
	}
	return existing, true
}

func (s *Server) handleUpdateFleetGroupIntegration() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "integrationId")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgIntegrationIDRequired)
			return
		}
		// R-S-14: ensure the operator owns the parent group before any
		// mutation. We resolve the existing record first so the scope
		// check reflects the integration's current fleet_group_id.
		if _, ok := s.loadIntegrationForScopedMutation(w, r, user, id, "get integration for update failed"); !ok {
			return
		}
		var request updateFleetGroupIntegrationRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid integration payload")
			return
		}
		record, err := s.fleetSvc.UpdateIntegration(r.Context(), id, fleet.UpdateIntegrationInput{
			ProviderID: request.ProviderID,
			Config:     request.Config,
			Enabled:    request.Enabled,
		})
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, "integration or provider not found")
				return
			}
			s.writeIntegrationError(w, err)
			return
		}
		s.appendAuditWithContext(r.Context(), session.UserID, "fleet_group_integrations.update", record.ID, map[string]any{
			"fleet_group_id": record.FleetGroupID,
			"kind":           record.Kind,
			"enabled":        record.Enabled,
		})
		writeJSON(w, http.StatusOK, fleetGroupIntegrationRecordToResponse(record))
	}
}

func (s *Server) handleDeleteFleetGroupIntegration() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "integrationId")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgIntegrationIDRequired)
			return
		}
		// R-S-14: scope-check the parent group before uninstalling.
		if _, ok := s.loadIntegrationForScopedMutation(w, r, user, id, "get integration for delete failed"); !ok {
			return
		}
		if err := s.fleetSvc.UninstallIntegration(r.Context(), id); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgIntegrationNotFound)
				return
			}
			s.logger.Error("delete integration failed", "id", id, "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		s.appendAuditWithContext(r.Context(), session.UserID, "fleet_group_integrations.delete", id, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

// fleetGroupIntegrationRecordToResponse produces the detail response
// matching the nested shape inside fleetGroupResponse.Integrations.
// Used by POST/GET/PATCH responses on the install endpoints.
func fleetGroupIntegrationRecordToResponse(i storage.FleetGroupIntegrationRecord) fleetGroupIntegrationResponse {
	providerID := ""
	if i.ProviderID != nil {
		providerID = *i.ProviderID
	}
	config := json.RawMessage(i.Config)
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	return fleetGroupIntegrationResponse{
		ID:            i.ID,
		Kind:          i.Kind,
		ProviderID:    providerID,
		Enabled:       i.Enabled,
		Config:        config,
		CreatedAtUnix: i.CreatedAt.UTC().Unix(),
		UpdatedAtUnix: i.UpdatedAt.UTC().Unix(),
	}
}

// writeIntegrationError maps registry-level validation errors to
// HTTP status codes.
func (s *Server) writeIntegrationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, integrations.ErrUnknownKind),
		errors.Is(err, integrations.ErrUnknownProviderKind),
		errors.Is(err, integrations.ErrProviderRequired),
		errors.Is(err, integrations.ErrProviderKindMismatch),
		errors.Is(err, integrations.ErrProviderNotApplicable):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		if isUniqueViolationFromStore(err) {
			writeError(w, http.StatusConflict, "integration already installed for this kind")
			return
		}
		s.logger.Error("integration mutation failed", "error", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
	}
}

// isUniqueViolationFromStore mirrors fleet.isUniqueViolation but
// lives in the server package so the HTTP handler can translate DB
// errors without reaching back into the fleet package.
func isUniqueViolationFromStore(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "SQLSTATE 23505")
}
