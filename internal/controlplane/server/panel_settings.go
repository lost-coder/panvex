package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const (
	panelTLSModeProxy  = "proxy"
	panelTLSModeDirect = "direct"
	// PanelRuntimeSourceLegacy reports that runtime values come from the legacy flag-based startup path.
	PanelRuntimeSourceLegacy = "legacy"
	// PanelRuntimeSourceConfigFile reports that runtime values come from config.toml.
	PanelRuntimeSourceConfigFile = "config_file"
)

// PanelRuntime describes the currently applied network and restart runtime.
type PanelRuntime struct {
	HTTPListenAddress  string
	HTTPRootPath       string
	AgentHTTPRootPath  string
	PanelAllowedCIDRs  []*net.IPNet
	GRPCListenAddress  string
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

func panelSettingsToRecord(settings PanelSettings) storage.PanelSettingsRecord {
	return storage.PanelSettingsRecord{
		HTTPPublicURL:      settings.HTTPPublicURL,
		GRPCPublicEndpoint: settings.GRPCPublicEndpoint,
		PasswordMinLength:  settings.PasswordMinLength,
		UpdatedAt:          time.Unix(settings.UpdatedAt, 0).UTC(),
	}
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

// UpdateSettings controls how the panel checks for and applies updates.
type UpdateSettings struct {
	CheckIntervalHours  int    `json:"check_interval_hours"`
	AutoUpdatePanel     bool   `json:"auto_update_panel"`
	AutoUpdateAgents    bool   `json:"auto_update_agents"`
	GitHubRepo          string `json:"github_repo"`
	GitHubToken         string `json:"github_token,omitempty"`
	AgentDownloadSource string `json:"agent_download_source"`
}

func defaultUpdateSettings() UpdateSettings {
	return UpdateSettings{
		CheckIntervalHours:  6,
		AutoUpdatePanel:     false,
		AutoUpdateAgents:    false,
		GitHubRepo:          "lost-coder/panvex",
		AgentDownloadSource: "github",
	}
}

// UpdateState caches the latest known versions from GitHub.
type UpdateState struct {
	LatestPanelVersion string `json:"latest_panel_version"`
	LatestAgentVersion string `json:"latest_agent_version"`
	PanelDownloadURL   string `json:"panel_download_url"`
	PanelChecksumURL   string `json:"panel_checksum_url"`
	PanelSignatureURL  string `json:"panel_signature_url"`
	AgentDownloadURL   string `json:"agent_download_url"`
	AgentChecksumURL   string `json:"agent_checksum_url"`
	AgentSignatureURL  string `json:"agent_signature_url"`
	PanelChangelog     string `json:"panel_changelog"`
	AgentChangelog     string `json:"agent_changelog"`
	LastCheckedAt      int64  `json:"last_checked_at"`
}

func (s *Server) restoreUpdateSettings() error {
	// ctx is the boot-time lifecycle context (s.serverCtx) so a Close()
	// during a slow update-settings/state read aborts the read instead of
	// holding the constructor open (Plan 3 / BP-01).
	ctx := s.Context()
	data, err := s.store.GetUpdateSettings(ctx)
	if err != nil {
		return err
	}
	if data != nil {
		if err := json.Unmarshal(data, &s.updateSettings); err != nil {
			return err
		}
	}
	data, err = s.store.GetUpdateState(ctx)
	if err != nil {
		return err
	}
	if data != nil {
		if err := json.Unmarshal(data, &s.updateState); err != nil {
			return err
		}
	}
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
