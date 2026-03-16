package server

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

const (
	panelTLSModeProxy  = "proxy"
	panelTLSModeDirect = "direct"
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
}

// PanelSettings stores operator-managed panel endpoint, listener, and TLS fields.
type PanelSettings struct {
	HTTPPublicURL      string `json:"http_public_url"`
	HTTPRootPath       string `json:"http_root_path"`
	GRPCPublicEndpoint string `json:"grpc_public_endpoint"`
	HTTPListenAddress  string `json:"http_listen_address"`
	GRPCListenAddress  string `json:"grpc_listen_address"`
	TLSMode            string `json:"tls_mode"`
	TLSCertFile        string `json:"tls_cert_file"`
	TLSKeyFile         string `json:"tls_key_file"`
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
	return runtime
}

func defaultPanelSettings(runtime PanelRuntime) PanelSettings {
	return PanelSettings{
		HTTPPublicURL:      "",
		HTTPRootPath:       runtime.HTTPRootPath,
		GRPCPublicEndpoint: "",
		HTTPListenAddress:  runtime.HTTPListenAddress,
		GRPCListenAddress:  runtime.GRPCListenAddress,
		TLSMode:            runtime.TLSMode,
		TLSCertFile:        runtime.TLSCertFile,
		TLSKeyFile:         runtime.TLSKeyFile,
		UpdatedAt:          0,
	}
}

func normalizePanelSettings(settings PanelSettings, runtime PanelRuntime) (PanelSettings, error) {
	settings.HTTPPublicURL = strings.TrimSpace(settings.HTTPPublicURL)
	settings.HTTPRootPath = normalizePanelRootPath(settings.HTTPRootPath)
	settings.GRPCPublicEndpoint = strings.TrimSpace(settings.GRPCPublicEndpoint)
	settings.HTTPListenAddress = strings.TrimSpace(settings.HTTPListenAddress)
	settings.GRPCListenAddress = strings.TrimSpace(settings.GRPCListenAddress)
	settings.TLSMode = normalizePanelTLSMode(settings.TLSMode)
	settings.TLSCertFile = strings.TrimSpace(settings.TLSCertFile)
	settings.TLSKeyFile = strings.TrimSpace(settings.TLSKeyFile)

	if settings.HTTPListenAddress == "" {
		settings.HTTPListenAddress = runtime.HTTPListenAddress
	}
	if settings.GRPCListenAddress == "" {
		settings.GRPCListenAddress = runtime.GRPCListenAddress
	}
	if settings.TLSMode == "" {
		settings.TLSMode = runtime.TLSMode
	}
	if settings.TLSMode != panelTLSModeProxy && settings.TLSMode != panelTLSModeDirect {
		return PanelSettings{}, errors.New("invalid tls mode")
	}
	if settings.TLSMode == panelTLSModeProxy {
		settings.TLSCertFile = ""
		settings.TLSKeyFile = ""
	}

	return settings, nil
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

func panelSettingsToRecord(settings PanelSettings) storage.PanelSettingsRecord {
	return storage.PanelSettingsRecord{
		HTTPPublicURL:      settings.HTTPPublicURL,
		HTTPRootPath:       settings.HTTPRootPath,
		GRPCPublicEndpoint: settings.GRPCPublicEndpoint,
		HTTPListenAddress:  settings.HTTPListenAddress,
		GRPCListenAddress:  settings.GRPCListenAddress,
		TLSMode:            settings.TLSMode,
		TLSCertFile:        settings.TLSCertFile,
		TLSKeyFile:         settings.TLSKeyFile,
		UpdatedAt:          time.Unix(settings.UpdatedAt, 0).UTC(),
	}
}

func panelSettingsFromRecord(record storage.PanelSettingsRecord) PanelSettings {
	return PanelSettings{
		HTTPPublicURL:      record.HTTPPublicURL,
		HTTPRootPath:       record.HTTPRootPath,
		GRPCPublicEndpoint: record.GRPCPublicEndpoint,
		HTTPListenAddress:  record.HTTPListenAddress,
		GRPCListenAddress:  record.GRPCListenAddress,
		TLSMode:            record.TLSMode,
		TLSCertFile:        record.TLSCertFile,
		TLSKeyFile:         record.TLSKeyFile,
		UpdatedAt:          record.UpdatedAt.UTC().Unix(),
	}
}

func (s *Server) restoreStoredPanelSettings() {
	if s.store == nil {
		return
	}

	record, err := s.store.GetPanelSettings(context.Background())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return
		}
		panic(err)
	}

	settings, err := normalizePanelSettings(panelSettingsFromRecord(record), s.panelRuntime)
	if err != nil {
		panic(err)
	}

	s.panelSettings = settings
}

func (s *Server) panelSettingsSnapshot() PanelSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.panelSettings
}

func (s *Server) panelRestartStatus(settings PanelSettings) panelRestartStatus {
	pending := settings.HTTPRootPath != s.panelRuntime.HTTPRootPath ||
		settings.HTTPListenAddress != s.panelRuntime.HTTPListenAddress ||
		settings.GRPCListenAddress != s.panelRuntime.GRPCListenAddress ||
		settings.TLSMode != s.panelRuntime.TLSMode ||
		settings.TLSCertFile != s.panelRuntime.TLSCertFile ||
		settings.TLSKeyFile != s.panelRuntime.TLSKeyFile

	state := "ready"
	if !s.panelRuntime.RestartSupported {
		state = "unavailable"
	} else if pending {
		state = "pending"
	}

	return panelRestartStatus{
		Supported: s.panelRuntime.RestartSupported,
		Pending:   pending,
		State:     state,
	}
}
