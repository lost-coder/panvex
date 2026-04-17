package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/security"
)

// versionResponse is the JSON shape returned by GET /api/version.
// commit_sha and build_time are only populated for Operator+ sessions; for
// viewers they are omitted via `omitempty` to avoid revealing build
// fingerprints that make targeted vulnerability research easier.
type versionResponse struct {
	Version   string `json:"version"`
	CommitSHA string `json:"commit_sha,omitempty"`
	BuildTime string `json:"build_time,omitempty"`
}

func (s *Server) handleVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := versionResponse{Version: s.version}

		// Gate commit_sha / build_time on Operator+. The handler is registered
		// in the `authenticated` group, so any caller reaching here has a
		// session; we still tolerate requireSession errors by defaulting to
		// the truncated response.
		_, user, err := s.requireSession(r)
		if err == nil && roleRank(user.Role) >= roleRank(auth.RoleOperator) {
			resp.CommitSHA = s.commitSHA
			resp.BuildTime = s.buildTime
		}

		writeJSON(w, http.StatusOK, resp)
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

		if req.GitHubRepo != nil {
			if err := validateGitHubRepo(*req.GitHubRepo); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
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

		go s.checkForUpdates(context.Background()) //nolint:gosec // intentionally detached from request lifecycle

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
		signatureURL := state.PanelSignatureURL

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
			downloadURL, checksumURL, signatureURL = ResolveAssetURLs(panel, "control-plane")
		}

		if downloadURL == "" {
			writeError(w, http.StatusBadRequest, "no download URL available for target version")
			return
		}
		if signatureURL == "" {
			// Refuse to install unsigned artifacts; the signature is the
			// primary defence against supply-chain tampering (SEC-02).
			writeError(w, http.StatusBadRequest, "release is missing a signature (.sig) asset; cannot install")
			return
		}

		writeJSON(w, http.StatusAccepted, panelUpdateResponse{
			Status: "updating",
			From:   s.version,
			To:     targetVersion,
		})

		go s.performPanelUpdate(session.UserID, targetVersion, downloadURL, checksumURL, signatureURL, settings.GitHubToken) //nolint:gosec // intentionally detached from request lifecycle
	}
}

// performPanelUpdate downloads, verifies (signature required, checksum as a
// secondary check), and installs a new panel binary, then requests a service
// restart.
func (s *Server) performPanelUpdate(actorID, targetVersion, downloadURL, checksumURL, signatureURL, token string) {
	ctx := context.Background()

	// Signature is mandatory — a missing or empty URL is a hard stop.
	if signatureURL == "" {
		s.logger.Error("panel update: refusing to install without signature URL")
		return
	}

	// Download checksum first (optional defence-in-depth; signature below is authoritative).
	var expectedChecksum string
	if checksumURL != "" {
		var err error
		expectedChecksum, err = DownloadChecksum(ctx, checksumURL, token)
		if err != nil {
			s.logger.Error("panel update: download checksum failed", "error", err)
			return
		}
	}

	archivePath, err := DownloadArchive(ctx, downloadURL, token)
	if err != nil {
		s.logger.Error("panel update: download archive failed", "error", err)
		return
	}
	defer func() { _ = os.Remove(archivePath) }()

	// Download the detached signature, then verify the archive against the
	// embedded public key. This is the primary integrity gate — any failure
	// refuses the update without falling back to checksum-only trust.
	sig, err := DownloadSignature(ctx, signatureURL, token)
	if err != nil {
		s.logger.Error("panel update: download signature failed", "error", err)
		return
	}
	archiveBytes, err := os.ReadFile(archivePath) //nolint:gosec // path created by DownloadArchive
	if err != nil {
		s.logger.Error("panel update: read archive for signature check failed", "error", err)
		return
	}
	if err := security.VerifyArtifactBytes(archiveBytes, sig); err != nil {
		s.logger.Error("panel update: signature verification failed", "error", err)
		return
	}

	if expectedChecksum != "" {
		if err := VerifyChecksum(archivePath, expectedChecksum); err != nil {
			s.logger.Error("panel update: checksum verification failed", "error", err)
			return
		}
	}

	binaryPath, err := ExtractBinaryFromArchive(archivePath)
	if err != nil {
		s.logger.Error("panel update: extract binary failed", "error", err)
		return
	}
	defer func() { _ = os.Remove(binaryPath) }()

	currentBinary, err := os.Executable()
	if err != nil {
		s.logger.Error("panel update: resolve current binary path failed", "error", err)
		return
	}

	if err := AtomicReplaceBinary(currentBinary, binaryPath); err != nil {
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

func (s *Server) handleAgentUpdate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := chi.URLParam(r, "id")

		s.mu.RLock()
		agent, exists := s.agents[agentID]
		s.mu.RUnlock()
		if !exists {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}

		var request struct {
			Version string `json:"version"`
		}
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}

		s.settingsMu.RLock()
		state := s.updateState
		settings := s.updateSettings
		s.settingsMu.RUnlock()

		targetVersion := request.Version
		if targetVersion == "" {
			targetVersion = state.LatestAgentVersion
		}
		if targetVersion == "" {
			writeError(w, http.StatusBadRequest, "no agent version available")
			return
		}

		downloadURL := state.AgentDownloadURL
		checksumURL := state.AgentChecksumURL
		signatureURL := state.AgentSignatureURL
		if downloadURL == "" {
			writeError(w, http.StatusBadRequest, "no download URL available")
			return
		}
		if signatureURL == "" {
			// Refuse to dispatch an unsigned agent update — the agent side
			// also refuses, but reject early so the operator sees the error
			// synchronously rather than via a failed job.
			writeError(w, http.StatusBadRequest, "release is missing a signature (.sig) asset; cannot update agent")
			return
		}

		// Fetch checksum from GitHub.
		checkCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		checksum, err := DownloadChecksum(checkCtx, checksumURL, settings.GitHubToken)
		if err != nil {
			s.logger.Error("agent update: fetch checksum failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to fetch checksum")
			return
		}

		// Build job payload.
		downloadViaPanel := settings.AgentDownloadSource == "panel"
		payload := map[string]any{
			"version":            targetVersion,
			"download_url":       downloadURL,
			"checksum_sha256":    checksum,
			"signature_url":      signatureURL,
			"download_via_panel": downloadViaPanel,
		}
		if downloadViaPanel {
			panelURL := s.panelSettings.HTTPPublicURL
			if panelURL == "" {
				panelURL = "http://" + r.Host
			}
			payload["panel_proxy_url"] = strings.TrimRight(panelURL, "/") +
				"/api/agent/update/binary?version=" + strings.TrimPrefix(targetVersion, "v") +
				"&arch=amd64"
		}
		payloadJSON, _ := json.Marshal(payload)

		// P1-SEC-11: never discard the requireSession error. Without this
		// check a malformed/expired cookie here would fall through with an
		// empty ActorID in both the job record and the audit event, making
		// the action untraceable. The handler sits in the operator group so
		// the middleware already gates access, but we verify again and fail
		// closed rather than silently anonymising a privileged action.
		session, _, err := s.requireSession(r)
		if err != nil {
			s.logger.Warn("agent update: session check failed in handler body", "error", err)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		job, err := s.jobs.Enqueue(jobs.CreateJobInput{
			Action:         jobs.ActionAgentSelfUpdate,
			TargetAgentIDs: []string{agentID},
			PayloadJSON:    string(payloadJSON),
			ActorID:        session.UserID,
		}, s.now())
		if err != nil {
			s.logger.Error("agent update: enqueue job failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create update job")
			return
		}

		s.notifyAgentSessions(job.TargetAgentIDs)
		s.appendAuditWithContext(r.Context(), session.UserID, "agents.update.dispatched", agentID, map[string]any{
			"version": targetVersion, "node_name": agent.NodeName,
		})

		writeJSON(w, http.StatusOK, map[string]any{
			"job_id":  job.ID,
			"status":  "dispatched",
			"version": targetVersion,
		})
	}
}

// allowedAgentArches constrains the arch query parameter on the agent
// binary proxy to known-safe values before it is interpolated into the
// GitHub release URL. Arbitrary values would let a caller fetch unexpected
// release assets or attempt path-like constructs in the URL.
var allowedAgentArches = map[string]struct{}{
	"amd64": {},
	"arm64": {},
}

func (s *Server) handleAgentBinaryProxy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		version := r.URL.Query().Get("version")
		arch := r.URL.Query().Get("arch")
		if version == "" || arch == "" {
			writeError(w, http.StatusBadRequest, "version and arch query parameters required")
			return
		}
		if _, ok := allowedAgentArches[arch]; !ok {
			writeError(w, http.StatusBadRequest, "unsupported arch; allowed: amd64, arm64")
			return
		}

		s.settingsMu.RLock()
		settings := s.updateSettings
		s.settingsMu.RUnlock()

		// Re-validate the stored repo before interpolating it into the URL,
		// in case an earlier code path bypassed handlePutUpdateSettings' check.
		if err := validateGitHubRepo(settings.GitHubRepo); err != nil {
			writeError(w, http.StatusBadRequest, "invalid github_repo configured")
			return
		}

		assetName := fmt.Sprintf("panvex-agent-linux-%s", arch)
		rawURL := fmt.Sprintf("https://github.com/%s/releases/download/agent/v%s/%s",
			settings.GitHubRepo, strings.TrimPrefix(version, "v"), assetName)
		if err := checkDownloadURL(rawURL); err != nil {
			writeError(w, http.StatusBadRequest, "invalid download URL")
			return
		}

		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, rawURL, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create request")
			return
		}
		if settings.GitHubToken != "" {
			req.Header.Set("Authorization", "Bearer "+settings.GitHubToken)
		}
		req.Header.Set("Accept", "application/octet-stream")

		// secureDownloadClient restricts redirects to the GitHub allow-list so
		// a rogue release asset cannot steer us toward an attacker host.
		resp, err := secureDownloadClient().Do(req)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to download from GitHub")
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			writeError(w, resp.StatusCode, "GitHub returned an error")
			return
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		if resp.ContentLength > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
		}
		w.WriteHeader(http.StatusOK)
		io.Copy(w, resp.Body) //nolint:errcheck
	}
}
