package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
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
	PasswordMinLength  int32              `json:"password_min_length"`
	UpdatedAtUnix      int64              `json:"updated_at_unix"`
	Restart            panelRestartStatus `json:"restart"`
}

type updatePanelSettingsRequest struct {
	HTTPPublicURL      string `json:"http_public_url"`
	GRPCPublicEndpoint string `json:"grpc_public_endpoint"`
	PasswordMinLength  int32  `json:"password_min_length"`
}

const (
	maxPanelSettingsBodyBytes = 16 * 1024
	errInvalidPanelSettings   = "invalid panel settings payload"
)

// panelSettingsFromStore builds a PanelSettings from the OperationalStore
// typed getters. Falls back to panelSettingsSnapshot when the store is nil
// (test fixtures without a DB-backed store).
func (s *Server) panelSettingsFromStore() PanelSettings {
	if s.settings == nil {
		return s.panelSettingsSnapshot()
	}
	return PanelSettings{
		HTTPPublicURL:      s.settings.HTTPPublicURL(),
		GRPCPublicEndpoint: s.settings.GRPCPublicEndpoint(),
		PasswordMinLength:  int32(s.settings.PasswordMinLength()), //nolint:gosec // bounded 8–64 in registry
	}
}

func (s *Server) handleGetPanelSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, msgAdminRoleRequired)
			return
		}

		settings := s.panelSettingsFromStore()
		writeJSON(w, http.StatusOK, panelSettingsResponseFromSettings(settings, s.panelRuntime, s.panelRestartStatus()))
	}
}

// readPanelSettingsBody reads, length-limits, and JSON-decodes the panel
// settings payload. Writes the HTTP error and returns false on any failure.
func readPanelSettingsBody(w http.ResponseWriter, r *http.Request) (updatePanelSettingsRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPanelSettingsBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "panel settings payload too large")
			return updatePanelSettingsRequest{}, false
		}
		writeError(w, http.StatusBadRequest, errInvalidPanelSettings)
		return updatePanelSettingsRequest{}, false
	}
	_ = r.Body.Close()

	var request updatePanelSettingsRequest
	if err := json.Unmarshal(body, &request); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPanelSettings)
		return updatePanelSettingsRequest{}, false
	}
	return request, true
}

func (s *Server) handlePutPanelSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, msgAdminRoleRequired)
			return
		}

		request, ok := readPanelSettingsBody(w, r)
		if !ok {
			return
		}

		if request.PasswordMinLength != 0 && (request.PasswordMinLength < 8 || request.PasswordMinLength > 128) {
			writeError(w, http.StatusBadRequest, "password_min_length must be between 8 and 128")
			return
		}

		// Preserve the existing password_min_length when the caller omits it (zero value).
		effectivePasswordMinLength := request.PasswordMinLength
		if effectivePasswordMinLength == 0 {
			if s.settings != nil {
				effectivePasswordMinLength = int32(s.settings.PasswordMinLength()) //nolint:gosec // bounded 8–64 in registry
			} else {
				effectivePasswordMinLength = s.panelSettingsSnapshot().PasswordMinLength
			}
		}

		settings := normalizePanelSettings(PanelSettings{
			HTTPPublicURL:      request.HTTPPublicURL,
			GRPCPublicEndpoint: request.GRPCPublicEndpoint,
			PasswordMinLength:  effectivePasswordMinLength,
			UpdatedAt:          s.now().UTC().Unix(),
		})

		// Dual-write: route through OperationalStore so /api/settings/values
		// stays consistent with this typed endpoint, then update the
		// in-memory snapshot below for subsystems (auth/enrollment) that
		// don't go through the store yet. Mirrors the retention/geoip
		// pattern; can be collapsed once boot init reorders to construct
		// OperationalStore before the snapshot consumers.
		if s.settings != nil {
			who := fmt.Sprintf("user:%s", session.UserID)
			updates := map[string]string{
				"http.public_url":          settings.HTTPPublicURL,
				"grpc.public_endpoint":     settings.GRPCPublicEndpoint,
				"auth.password_min_length": fmt.Sprintf("%d", settings.PasswordMinLength),
			}
			if err := s.settings.Put(r.Context(), updates, who); err != nil {
				s.logger.Error("put panel settings failed", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}

		// Keep in-memory snapshot in sync so enrollment/auth handlers see
		// the new values without re-reading from the store.
		s.settingsMu.Lock()
		s.panelSettings = settings
		s.settingsMu.Unlock()

		s.auth.SetPasswordPolicy(settings.PasswordMinLength)

		s.appendAuditWithContext(r.Context(), session.UserID, "settings.panel.update", "panel", map[string]any{
			"http_public_url":      settings.HTTPPublicURL,
			"grpc_public_endpoint": settings.GRPCPublicEndpoint,
			"password_min_length":  settings.PasswordMinLength,
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
			writeError(w, http.StatusForbidden, msgAdminRoleRequired)
			return
		}

		settings := s.panelSettingsFromStore()
		restart := s.panelRestartStatus()
		if !restart.Supported || s.requestRestart == nil {
			writeError(w, http.StatusConflict, "panel restart is unavailable in the current runtime")
			return
		}

		if err := s.requestRestart(); err != nil {
			s.logger.Error("panel restart request failed", "error", err)
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
	passwordMinLength := settings.PasswordMinLength
	if passwordMinLength == 0 {
		passwordMinLength = auth.DefaultPasswordMinLength
	}
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
		PasswordMinLength:  passwordMinLength,
		UpdatedAtUnix:      settings.UpdatedAt,
		Restart:            restart,
	}
}
