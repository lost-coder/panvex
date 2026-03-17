package server

import (
	"context"
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
	UpdatedAtUnix      int64              `json:"updated_at_unix"`
	Restart            panelRestartStatus `json:"restart"`
}

type updatePanelSettingsRequest struct {
	HTTPPublicURL      string `json:"http_public_url"`
	HTTPRootPath       string `json:"http_root_path"`
	GRPCPublicEndpoint string `json:"grpc_public_endpoint"`
	HTTPListenAddress  string `json:"http_listen_address"`
	GRPCListenAddress  string `json:"grpc_listen_address"`
	TLSMode            string `json:"tls_mode"`
	TLSCertFile        string `json:"tls_cert_file"`
	TLSKeyFile         string `json:"tls_key_file"`
}

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
		writeJSON(w, http.StatusOK, panelSettingsResponseFromSettings(settings, s.panelRestartStatus(settings)))
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

		var request updatePanelSettingsRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid panel settings payload")
			return
		}

		settings, err := normalizePanelSettings(PanelSettings{
			HTTPPublicURL:      request.HTTPPublicURL,
			HTTPRootPath:       request.HTTPRootPath,
			GRPCPublicEndpoint: request.GRPCPublicEndpoint,
			HTTPListenAddress:  request.HTTPListenAddress,
			GRPCListenAddress:  request.GRPCListenAddress,
			TLSMode:            request.TLSMode,
			TLSCertFile:        request.TLSCertFile,
			TLSKeyFile:         request.TLSKeyFile,
			UpdatedAt:          s.now().UTC().Unix(),
		}, s.panelRuntime)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if s.store != nil {
			if err := s.store.PutPanelSettings(context.Background(), panelSettingsToRecord(settings)); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}

		s.mu.Lock()
		s.panelSettings = settings
		s.mu.Unlock()

		s.appendAudit(session.UserID, "settings.panel.update", "panel", map[string]any{
			"http_public_url":      settings.HTTPPublicURL,
			"http_root_path":       settings.HTTPRootPath,
			"grpc_public_endpoint": settings.GRPCPublicEndpoint,
			"http_listen_address":  settings.HTTPListenAddress,
			"grpc_listen_address":  settings.GRPCListenAddress,
			"tls_mode":             settings.TLSMode,
		})

		restart := s.panelRestartStatus(settings)
		writeJSON(w, http.StatusOK, panelSettingsResponseFromSettings(settings, restart))
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
		restart := s.panelRestartStatus(settings)
		if !restart.Supported || s.requestRestart == nil {
			writeError(w, http.StatusConflict, "panel restart is unavailable in the current runtime")
			return
		}

		s.appendAudit(session.UserID, "settings.panel.restart", "panel", map[string]any{
			"pending_restart": restart.Pending,
		})

		writeJSON(w, http.StatusAccepted, panelSettingsResponseFromSettings(settings, restart))

		go func() {
			_ = s.requestRestart()
		}()
	}
}

func panelSettingsResponseFromSettings(settings PanelSettings, restart panelRestartStatus) panelSettingsResponse {
	return panelSettingsResponse{
		HTTPPublicURL:      settings.HTTPPublicURL,
		HTTPRootPath:       settings.HTTPRootPath,
		GRPCPublicEndpoint: settings.GRPCPublicEndpoint,
		HTTPListenAddress:  settings.HTTPListenAddress,
		GRPCListenAddress:  settings.GRPCListenAddress,
		TLSMode:            settings.TLSMode,
		TLSCertFile:        settings.TLSCertFile,
		TLSKeyFile:         settings.TLSKeyFile,
		UpdatedAtUnix:      settings.UpdatedAt,
		Restart:            restart,
	}
}
