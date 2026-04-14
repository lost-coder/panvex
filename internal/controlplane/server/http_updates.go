package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type versionResponse struct {
	Version   string `json:"version"`
	CommitSHA string `json:"commit_sha"`
	BuildTime string `json:"build_time"`
}

func (s *Server) handleVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, versionResponse{
			Version:   s.version,
			CommitSHA: s.commitSHA,
			BuildTime: s.buildTime,
		})
	}
}

// updateSettingsResponse is the JSON payload returned by GET /settings/updates.
type updateSettingsResponse struct {
	CheckIntervalHours  int    `json:"check_interval_hours"`
	AutoUpdatePanel     bool   `json:"auto_update_panel"`
	AutoUpdateAgents    bool   `json:"auto_update_agents"`
	GitHubRepo          string `json:"github_repo"`
	GitHubToken         string `json:"github_token"`
	AgentDownloadSource string `json:"agent_download_source"`
	CurrentVersion      string `json:"current_version"`
	State               UpdateState `json:"state"`
}

// updateSettingsRequest is the JSON payload accepted by PUT /settings/updates.
type updateSettingsRequest struct {
	CheckIntervalHours  *int    `json:"check_interval_hours,omitempty"`
	AutoUpdatePanel     *bool   `json:"auto_update_panel,omitempty"`
	AutoUpdateAgents    *bool   `json:"auto_update_agents,omitempty"`
	GitHubRepo          *string `json:"github_repo,omitempty"`
	GitHubToken         *string `json:"github_token,omitempty"`
	AgentDownloadSource *string `json:"agent_download_source,omitempty"`
}

// panelUpdateRequest is the JSON payload accepted by POST /panel/update.
type panelUpdateRequest struct {
	TargetVersion string `json:"target_version"`
}

// panelUpdateResponse is the JSON payload returned immediately by POST /panel/update.
type panelUpdateResponse struct {
	Status string `json:"status"`
	From   string `json:"from"`
	To     string `json:"to"`
}

func (s *Server) handleGetUpdateSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.settingsMu.RLock()
		settings := s.updateSettings
		state := s.updateState
		s.settingsMu.RUnlock()

		token := ""
		if settings.GitHubToken != "" {
			token = maskToken(settings.GitHubToken)
		}

		writeJSON(w, http.StatusOK, updateSettingsResponse{
			CheckIntervalHours:  settings.CheckIntervalHours,
			AutoUpdatePanel:     settings.AutoUpdatePanel,
			AutoUpdateAgents:    settings.AutoUpdateAgents,
			GitHubRepo:          settings.GitHubRepo,
			GitHubToken:         token,
			AgentDownloadSource: settings.AgentDownloadSource,
			CurrentVersion:      s.version,
			State:               state,
		})
	}
}

func (s *Server) handlePutUpdateSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req updateSettingsRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request payload")
			return
		}

		s.settingsMu.Lock()
		if req.CheckIntervalHours != nil {
			s.updateSettings.CheckIntervalHours = *req.CheckIntervalHours
		}
		if req.AutoUpdatePanel != nil {
			s.updateSettings.AutoUpdatePanel = *req.AutoUpdatePanel
		}
		if req.AutoUpdateAgents != nil {
			s.updateSettings.AutoUpdateAgents = *req.AutoUpdateAgents
		}
		if req.GitHubRepo != nil {
			s.updateSettings.GitHubRepo = *req.GitHubRepo
		}
		if req.GitHubToken != nil {
			// Preserve existing token if the client sends the masked placeholder.
			if *req.GitHubToken != "***" && !strings.HasSuffix(*req.GitHubToken, "...") {
				s.updateSettings.GitHubToken = *req.GitHubToken
			}
		}
		if req.AgentDownloadSource != nil {
			s.updateSettings.AgentDownloadSource = *req.AgentDownloadSource
		}
		updated := s.updateSettings
		s.settingsMu.Unlock()

		if s.store != nil {
			data, _ := json.Marshal(updated)
			if err := s.store.PutUpdateSettings(r.Context(), data); err != nil {
				s.logger.Error("persist update settings failed", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "settings.updates.update", "panel", map[string]any{
			"github_repo":      updated.GitHubRepo,
			"auto_update_panel": updated.AutoUpdatePanel,
		})

		s.settingsMu.RLock()
		state := s.updateState
		s.settingsMu.RUnlock()

		token := ""
		if updated.GitHubToken != "" {
			token = maskToken(updated.GitHubToken)
		}

		writeJSON(w, http.StatusOK, updateSettingsResponse{
			CheckIntervalHours:  updated.CheckIntervalHours,
			AutoUpdatePanel:     updated.AutoUpdatePanel,
			AutoUpdateAgents:    updated.AutoUpdateAgents,
			GitHubRepo:          updated.GitHubRepo,
			GitHubToken:         token,
			AgentDownloadSource: updated.AgentDownloadSource,
			CurrentVersion:      s.version,
			State:               state,
		})
	}
}

func (s *Server) handleForceUpdateCheck() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "settings.updates.check", "panel", nil)

		go s.checkForUpdates(context.Background())

		writeJSON(w, http.StatusAccepted, map[string]string{"status": "checking"})
	}
}

func (s *Server) handlePanelUpdate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req panelUpdateRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request payload")
			return
		}

		s.settingsMu.RLock()
		settings := s.updateSettings
		state := s.updateState
		s.settingsMu.RUnlock()

		targetVersion := strings.TrimSpace(req.TargetVersion)
		if targetVersion == "" {
			targetVersion = state.LatestPanelVersion
		}
		if targetVersion == "" {
			writeError(w, http.StatusBadRequest, "no target version specified and no latest version cached")
			return
		}

		// Strip "v" prefix for comparison since UpdateState stores bare semver.
		currentVersion := strings.TrimPrefix(s.version, "v")
		if CompareVersions(targetVersion, currentVersion) <= 0 {
			writeError(w, http.StatusConflict, "target version is not newer than current version")
			return
		}

		downloadURL := state.PanelDownloadURL
		checksumURL := state.PanelChecksumURL

		// If the target version differs from the cached latest, fetch the
		// specific release from GitHub to resolve asset URLs.
		if targetVersion != state.LatestPanelVersion {
			tag := "control-plane/v" + targetVersion
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()

			panel, _, err := FetchLatestVersions(ctx, settings.GitHubRepo, settings.GitHubToken)
			if err != nil || panel == nil || panel.TagName != tag {
				writeError(w, http.StatusBadGateway, "failed to resolve download URLs for target version")
				return
			}
			downloadURL, checksumURL = ResolveAssetURLs(panel, "control-plane")
		}

		if downloadURL == "" {
			writeError(w, http.StatusBadRequest, "no download URL available for target version")
			return
		}

		writeJSON(w, http.StatusAccepted, panelUpdateResponse{
			Status: "updating",
			From:   s.version,
			To:     targetVersion,
		})

		go s.performPanelUpdate(session.UserID, targetVersion, downloadURL, checksumURL, settings.GitHubToken)
	}
}

// performPanelUpdate downloads, verifies, and installs a new panel binary,
// then requests a service restart.
func (s *Server) performPanelUpdate(actorID, targetVersion, downloadURL, checksumURL, token string) {
	ctx := context.Background()

	// Download checksum if available.
	var expectedChecksum string
	if checksumURL != "" {
		var err error
		expectedChecksum, err = DownloadChecksum(ctx, checksumURL, token)
		if err != nil {
			s.logger.Error("panel update: download checksum failed", "error", err)
			return
		}
	}

	tmpPath, err := DownloadBinary(ctx, downloadURL, token)
	if err != nil {
		s.logger.Error("panel update: download binary failed", "error", err)
		return
	}
	defer func() {
		// Clean up temp file if it still exists (replace succeeded = moved away).
		os.Remove(tmpPath)
	}()

	if expectedChecksum != "" {
		if err := VerifyChecksum(tmpPath, expectedChecksum); err != nil {
			s.logger.Error("panel update: checksum verification failed", "error", err)
			return
		}
	}

	currentBinary, err := os.Executable()
	if err != nil {
		s.logger.Error("panel update: resolve current binary path failed", "error", err)
		return
	}

	if err := AtomicReplaceBinary(currentBinary, tmpPath); err != nil {
		s.logger.Error("panel update: atomic replace failed", "error", err)
		return
	}

	s.appendAudit(actorID, "panel.update.applied", "panel", map[string]any{
		"from_version": s.version,
		"to_version":   targetVersion,
	})

	s.logger.Info("panel binary updated, requesting restart", "from", s.version, "to", targetVersion)

	if s.requestRestart != nil {
		if err := s.requestRestart(); err != nil {
			s.logger.Error("panel update: restart request failed", "error", err)
		}
	}
}
