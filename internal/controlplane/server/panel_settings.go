package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
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
	HTTPListenAddress string
	HTTPRootPath      string
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

func buildPanelPublicURL(settings PanelSettings, runtime PanelRuntime, requestURL *url.URL, forwardedProto string, requestHost string) string {
	base := strings.TrimSpace(settings.HTTPPublicURL)
	if base == "" {
		scheme := strings.TrimSpace(forwardedProto)
		if scheme == "" && requestURL != nil {
			scheme = strings.TrimSpace(requestURL.Scheme)
		}
		if scheme == "" {
			if runtime.TLSMode == panelTLSModeDirect {
				scheme = "https"
			} else {
				scheme = "http"
			}
		}

		host := strings.TrimSpace(requestHost)
		if host == "" && requestURL != nil {
			host = strings.TrimSpace(requestURL.Host)
		}
		if host == "" {
			return ""
		}

		base = fmt.Sprintf("%s://%s", scheme, host)
	}

	if runtime.HTTPRootPath == "" {
		return strings.TrimRight(base, "/")
	}

	return strings.TrimRight(base, "/") + runtime.HTTPRootPath
}

func panelSettingsToRecord(settings PanelSettings) storage.PanelSettingsRecord {
	return storage.PanelSettingsRecord{
		HTTPPublicURL:      settings.HTTPPublicURL,
		GRPCPublicEndpoint: settings.GRPCPublicEndpoint,
		UpdatedAt:          time.Unix(settings.UpdatedAt, 0).UTC(),
	}
}

func panelSettingsFromRecord(record storage.PanelSettingsRecord) PanelSettings {
	return PanelSettings{
		HTTPPublicURL:      record.HTTPPublicURL,
		GRPCPublicEndpoint: record.GRPCPublicEndpoint,
		UpdatedAt:          record.UpdatedAt.UTC().Unix(),
	}
}

func (s *Server) restoreStoredPanelSettings() error {
	if s.store == nil {
		return nil
	}

	record, err := s.store.GetPanelSettings(context.Background())
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
		GitHubRepo:          "panvex/panvex",
		AgentDownloadSource: "github",
	}
}

// UpdateState caches the latest known versions from GitHub.
type UpdateState struct {
	LatestPanelVersion string `json:"latest_panel_version"`
	LatestAgentVersion string `json:"latest_agent_version"`
	PanelDownloadURL   string `json:"panel_download_url"`
	PanelChecksumURL   string `json:"panel_checksum_url"`
	AgentDownloadURL   string `json:"agent_download_url"`
	AgentChecksumURL   string `json:"agent_checksum_url"`
	PanelChangelog     string `json:"panel_changelog"`
	AgentChangelog     string `json:"agent_changelog"`
	LastCheckedAt      int64  `json:"last_checked_at"`
}

func (s *Server) restoreUpdateSettings() error {
	data, err := s.store.GetUpdateSettings(context.Background())
	if err != nil {
		return err
	}
	if data != nil {
		if err := json.Unmarshal(data, &s.updateSettings); err != nil {
			return err
		}
	}
	data, err = s.store.GetUpdateState(context.Background())
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
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()

	return s.panelSettings
}

func (s *Server) panelRestartStatus() panelRestartStatus {
	state := "ready"
	if !s.panelRuntime.RestartSupported {
		state = "unavailable"
	}

	return panelRestartStatus{
		Supported: s.panelRuntime.RestartSupported,
		Pending:   false,
		State:     state,
	}
}
