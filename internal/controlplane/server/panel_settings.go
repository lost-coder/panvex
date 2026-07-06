package server

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

const (
	panelTLSModeProxy  = "proxy"
	panelTLSModeDirect = "direct"
	// PanelRuntimeSourceLegacy reports that runtime values come from the
	// flag/env-based startup path (no config.toml). NOT dead code: it is
	// the live default for flag-driven deployments — see
	// cmd/control-plane/serve.go (ConfigSource wiring) and
	// defaultPanelRuntime below. "Legacy" only contrasts it with the
	// config-file path. (Audit 2026-06-09 B7: verified load-bearing.)
	PanelRuntimeSourceLegacy = "legacy"
	// PanelRuntimeSourceConfigFile reports that runtime values come from config.toml.
	PanelRuntimeSourceConfigFile = "config_file"
)

// PanelRuntime describes the currently applied network and restart runtime.
type PanelRuntime struct {
	HTTPListenAddress string
	HTTPRootPath      string
	AgentHTTPRootPath string
	PanelAllowedCIDRs []*net.IPNet
	GRPCListenAddress string
	TLSMode           string
	TLSCertFile       string
	TLSKeyFile        string
	RestartSupported  bool
	ConfigSource      string
	ConfigPath        string
}

// PanelSettings stores operator-managed public access settings for the panel.
type PanelSettings struct {
	HTTPPublicURL      string `json:"http_public_url"`
	GRPCPublicEndpoint string `json:"grpc_public_endpoint"`
	PasswordMinLength  int32  `json:"password_min_length"`
	UpdatedAt          int64  `json:"updated_at_unix"`
}

type panelRestartStatus struct {
	Supported bool   `json:"supported"`
	Pending   bool   `json:"pending"`
	State     string `json:"state"`
}

func defaultPanelRuntime(runtime PanelRuntime) PanelRuntime {
	// Plan 6: listen addresses are resolved from the live settings store via
	// EffectiveHTTP/GRPCListenAddress. These PanelRuntime defaults remain only
	// as the no-store fallback (test fixtures without a DB-backed store).
	if strings.TrimSpace(runtime.HTTPListenAddress) == "" {
		runtime.HTTPListenAddress = ":8080"
	}
	if strings.TrimSpace(runtime.GRPCListenAddress) == "" {
		runtime.GRPCListenAddress = ":8443"
	}
	runtime.HTTPRootPath = normalizePanelRootPath(runtime.HTTPRootPath)
	runtime.TLSMode = normalizePanelTLSMode(runtime.TLSMode)
	if strings.TrimSpace(runtime.ConfigSource) == "" {
		runtime.ConfigSource = PanelRuntimeSourceLegacy
	}
	runtime.ConfigPath = strings.TrimSpace(runtime.ConfigPath)
	return runtime
}

func defaultPanelSettings() PanelSettings {
	return PanelSettings{
		HTTPPublicURL:      "",
		GRPCPublicEndpoint: "",
		UpdatedAt:          0,
	}
}

func normalizePanelSettings(settings PanelSettings) PanelSettings {
	settings.HTTPPublicURL = strings.TrimSpace(settings.HTTPPublicURL)
	settings.GRPCPublicEndpoint = strings.TrimSpace(settings.GRPCPublicEndpoint)
	return settings
}

func normalizePanelTLSMode(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return panelTLSModeProxy
	}
	return normalized
}

func normalizePanelRootPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "/" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return cleaned
}

// resolvePanelBaseURL returns the configured public URL or, when
// unset, derives a base from the incoming request (scheme + host).
// Returns "" only when neither configuration nor the request supply a
// usable host.
func resolvePanelBaseURL(settings PanelSettings, runtime PanelRuntime, requestURL *url.URL, forwardedProto string, requestHost string) string {
	base := strings.TrimSpace(settings.HTTPPublicURL)
	if base != "" {
		return base
	}
	scheme := resolvePanelScheme(forwardedProto, requestURL, runtime)
	host := resolvePanelHost(requestHost, requestURL)
	if host == "" {
		return ""
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func resolvePanelScheme(forwardedProto string, requestURL *url.URL, runtime PanelRuntime) string {
	scheme := strings.TrimSpace(forwardedProto)
	if scheme == "" && requestURL != nil {
		scheme = strings.TrimSpace(requestURL.Scheme)
	}
	if scheme != "" {
		return scheme
	}
	if runtime.TLSMode == panelTLSModeDirect {
		return "https"
	}
	return "http"
}

func resolvePanelHost(requestHost string, requestURL *url.URL) string {
	host := strings.TrimSpace(requestHost)
	if host == "" && requestURL != nil {
		host = strings.TrimSpace(requestURL.Host)
	}
	return host
}

func buildPanelPublicURL(settings PanelSettings, runtime PanelRuntime, requestURL *url.URL, forwardedProto string, requestHost string) string {
	base := resolvePanelBaseURL(settings, runtime, requestURL, forwardedProto, requestHost)
	if base == "" {
		return ""
	}
	if runtime.HTTPRootPath == "" {
		return strings.TrimRight(base, "/")
	}
	return strings.TrimRight(base, "/") + runtime.HTTPRootPath
}

func buildAgentPublicURL(settings PanelSettings, runtime PanelRuntime, requestURL *url.URL, forwardedProto string, requestHost string) string {
	if runtime.AgentHTTPRootPath == "" {
		return buildPanelPublicURL(settings, runtime, requestURL, forwardedProto, requestHost)
	}

	base := resolvePanelBaseURL(settings, runtime, requestURL, forwardedProto, requestHost)
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + runtime.AgentHTTPRootPath
}

func panelSettingsFromRecord(record storage.PanelSettingsRecord) PanelSettings {
	return PanelSettings{
		HTTPPublicURL:      record.HTTPPublicURL,
		GRPCPublicEndpoint: record.GRPCPublicEndpoint,
		PasswordMinLength:  record.PasswordMinLength,
		UpdatedAt:          record.UpdatedAt.UTC().Unix(),
	}
}

func (s *Server) restoreStoredPanelSettings() error {
	if s.store == nil {
		return nil
	}

	// ctx is the boot-time lifecycle context (s.serverCtx) so a Close()
	// during a slow GetPanelSettings storage call aborts the read instead
	// of holding the constructor open (Plan 3 / BP-01).
	record, err := s.store.GetPanelSettings(s.Context())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		return err
	}

	s.panelSettings = normalizePanelSettings(panelSettingsFromRecord(record))
	return nil
}

// UpdateSettings / UpdateState moved to internal/controlplane/updates with the
// updates.Service extraction (P8.2i). server keeps aliases so its call sites
// (panel_settings.go, http_updates.go, update_checker.go, tests) compile
// unchanged.
type (
	UpdateSettings = updates.Settings
	UpdateState    = updates.State
)

func (s *Server) restoreUpdateSettings() error {
	// updatesSvc is nil exactly when no persistent store is wired; the
	// no-store path keeps the defaults stamped in newServerFromOptions.
	if s.updatesSvc == nil {
		return nil
	}
	// ctx is the boot-time lifecycle context (s.serverCtx) so a Close()
	// during a slow update-settings/state read aborts the read instead of
	// holding the constructor open (Plan 3 / BP-01).
	ctx := s.Context()
	settings, err := s.updatesSvc.LoadSettings(ctx)
	if err != nil {
		return err
	}
	state, err := s.updatesSvc.LoadState(ctx)
	if err != nil {
		return err
	}
	s.updateSettings = settings
	s.updateState = state
	return nil
}

func (s *Server) panelSettingsSnapshot() PanelSettings {
	// Store-backed path is authoritative: every consumer (enrollment URL
	// builders, auth password policy) reads the live OperationalStore, so a
	// value saved through EITHER /api/settings/values or /api/settings/panel
	// is reflected immediately. The legacy s.panelSettings struct is only the
	// fallback when no store is wired (test fixtures).
	if s.settings != nil {
		return normalizePanelSettings(PanelSettings{
			HTTPPublicURL:      s.settings.HTTPPublicURL(),
			GRPCPublicEndpoint: s.settings.GRPCPublicEndpoint(),
			PasswordMinLength:  int32(s.settings.PasswordMinLength()), //nolint:gosec // bounded 8–64 in registry
		})
	}
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.panelSettings
}

func (s *Server) panelRestartStatus() panelRestartStatus {
	state := "ready"
	if !s.panelRuntime.RestartSupported {
		state = "unavailable"
	}
	pending := false
	if s.settings != nil && s.settingsActive != nil {
		pending = len(s.settings.PendingChanges(s.settingsActive)) > 0
	}
	return panelRestartStatus{
		Supported: s.panelRuntime.RestartSupported,
		Pending:   pending,
		State:     state,
	}
}
