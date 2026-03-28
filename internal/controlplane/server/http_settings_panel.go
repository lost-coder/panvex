package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/panvex/panvex/internal/controlplane/auth"
)

type panelSettingsResponse struct {
	HTTPPublicURL      string             `json:"http_public_url"`
	HTTPRootPath       string             `json:"http_root_path"`
	GRPCPublicEndpoint string             `json:"grpc_public_endpoint"`
	HTTPListenAddress  string             `json:"http_listen_address"`
	GRPCListenAddress  string             `json:"grpc_listen_address"`
	TLSMode            string             `json:"tls_mode"`
	TLSCertFile        string             `json:"tls_cert_file"`
	TLSKeyFile         string             `json:"tls_key_file"`
	RuntimeSource      string             `json:"runtime_source"`
	RuntimeConfigPath  string             `json:"runtime_config_path"`
	UpdatedAtUnix      int64              `json:"updated_at_unix"`
	Restart            panelRestartStatus `json:"restart"`
}

type updatePanelSettingsRequest struct {
	HTTPPublicURL      string `json:"http_public_url"`
	GRPCPublicEndpoint string `json:"grpc_public_endpoint"`
}

const maxPanelSettingsBodyBytes = 16 * 1024

func (s *Server) handleGetPanelSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}

		settings := s.panelSettingsSnapshot()
		writeJSON(w, http.StatusOK, panelSettingsResponseFromSettings(settings, s.panelRuntime, s.panelRestartStatus()))
	}
}

func (s *Server) handlePutPanelSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxPanelSettingsBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				writeError(w, http.StatusRequestEntityTooLarge, "panel settings payload too large")
				return
			}
			writeError(w, http.StatusBadRequest, "invalid panel settings payload")
			return
		}
		_ = r.Body.Close()

		var request updatePanelSettingsRequest
		if err := json.Unmarshal(body, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid panel settings payload")
			return
		}

		var requestFields map[string]json.RawMessage
		if err := json.Unmarshal(body, &requestFields); err != nil {
			writeError(w, http.StatusBadRequest, "invalid panel settings payload")
			return
		}
		if err := rejectRuntimeMutation(requestFields, s.panelRuntime); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		settings := normalizePanelSettings(PanelSettings{
			HTTPPublicURL:      request.HTTPPublicURL,
			GRPCPublicEndpoint: request.GRPCPublicEndpoint,
			UpdatedAt:          s.now().UTC().Unix(),
		})

		if s.store != nil {
			if err := s.store.PutPanelSettings(r.Context(), panelSettingsToRecord(settings)); err != nil {
				log.Printf("put panel settings failed: %v", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}

		s.mu.Lock()
		s.panelSettings = settings
		s.mu.Unlock()

		s.appendAuditWithContext(r.Context(), session.UserID, "settings.panel.update", "panel", map[string]any{
			"http_public_url":      settings.HTTPPublicURL,
			"grpc_public_endpoint": settings.GRPCPublicEndpoint,
		})

		restart := s.panelRestartStatus()
		writeJSON(w, http.StatusOK, panelSettingsResponseFromSettings(settings, s.panelRuntime, restart))
	}
}

func (s *Server) handleRestartPanel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}

		settings := s.panelSettingsSnapshot()
		restart := s.panelRestartStatus()
		if !restart.Supported || s.requestRestart == nil {
			writeError(w, http.StatusConflict, "panel restart is unavailable in the current runtime")
			return
		}

		if err := s.requestRestart(); err != nil {
			log.Printf("panel restart request failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "settings.panel.restart", "panel", map[string]any{
			"pending_restart": restart.Pending,
		})

		writeJSON(w, http.StatusAccepted, panelSettingsResponseFromSettings(settings, s.panelRuntime, restart))
	}
}

func panelSettingsResponseFromSettings(settings PanelSettings, runtime PanelRuntime, restart panelRestartStatus) panelSettingsResponse {
	return panelSettingsResponse{
		HTTPPublicURL:      settings.HTTPPublicURL,
		HTTPRootPath:       runtime.HTTPRootPath,
		GRPCPublicEndpoint: settings.GRPCPublicEndpoint,
		HTTPListenAddress:  runtime.HTTPListenAddress,
		GRPCListenAddress:  runtime.GRPCListenAddress,
		TLSMode:            runtime.TLSMode,
		TLSCertFile:        runtime.TLSCertFile,
		TLSKeyFile:         runtime.TLSKeyFile,
		RuntimeSource:      runtime.ConfigSource,
		RuntimeConfigPath:  runtime.ConfigPath,
		UpdatedAtUnix:      settings.UpdatedAt,
		Restart:            restart,
	}
}

func rejectRuntimeMutation(requestFields map[string]json.RawMessage, runtime PanelRuntime) error {
	for _, field := range []string{
		"http_root_path",
		"http_listen_address",
		"grpc_listen_address",
		"tls_mode",
		"tls_cert_file",
		"tls_key_file",
	} {
		if _, exists := requestFields[field]; !exists {
			continue
		}
		if runtime.ConfigSource == PanelRuntimeSourceConfigFile {
			return errors.New(field + " is managed by the panel config file")
		}
		return errors.New(field + " is a read-only runtime value")
	}
	return nil
}
