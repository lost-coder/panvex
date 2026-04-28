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

// mergeUpdateSettings applies non-nil fields from req onto current. Caller
// holds settingsMu for write. Splitting this out keeps
// handlePutUpdateSettings below S3776's cognitive-complexity ceiling.
func mergeUpdateSettings(current *UpdateSettings, req updateSettingsRequest) {
	if req.CheckIntervalHours != nil {
		current.CheckIntervalHours = *req.CheckIntervalHours
	}
	if req.AutoUpdatePanel != nil {
		current.AutoUpdatePanel = *req.AutoUpdatePanel
	}
	if req.AutoUpdateAgents != nil {
		current.AutoUpdateAgents = *req.AutoUpdateAgents
	}
	if req.GitHubRepo != nil {
		current.GitHubRepo = *req.GitHubRepo
	}
	if req.GitHubToken != nil {
		// Preserve existing token if the client sends the masked placeholder.
		if *req.GitHubToken != "***" && !strings.HasSuffix(*req.GitHubToken, "...") {
			current.GitHubToken = *req.GitHubToken
		}
	}
	if req.AgentDownloadSource != nil {
		current.AgentDownloadSource = *req.AgentDownloadSource
	}
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

		req, ok := decodeUpdateSettingsRequest(w, r)
		if !ok {
			return
		}

		updated := s.applyUpdateSettings(req)
		if !s.persistUpdateSettings(w, r, updated) {
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "settings.updates.update", "panel", map[string]any{
			"github_repo":      updated.GitHubRepo,
			"auto_update_panel": updated.AutoUpdatePanel,
		})

		writeJSON(w, http.StatusOK, s.buildUpdateSettingsResponse(updated))
	}
}

func decodeUpdateSettingsRequest(w http.ResponseWriter, r *http.Request) (updateSettingsRequest, bool) {
	var req updateSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return updateSettingsRequest{}, false
	}
	if req.GitHubRepo != nil {
		if err := validateGitHubRepo(*req.GitHubRepo); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return updateSettingsRequest{}, false
		}
	}
	return req, true
}

func (s *Server) applyUpdateSettings(req updateSettingsRequest) UpdateSettings {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	mergeUpdateSettings(&s.updateSettings, req)
	return s.updateSettings
}

func (s *Server) persistUpdateSettings(w http.ResponseWriter, r *http.Request, updated UpdateSettings) bool {
	if s.store == nil {
		return true
	}
	data, _ := json.Marshal(updated)
	if err := s.store.PutUpdateSettings(r.Context(), data); err != nil {
		s.logger.Error("persist update settings failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return false
	}
	return true
}

func (s *Server) buildUpdateSettingsResponse(updated UpdateSettings) updateSettingsResponse {
	s.settingsMu.RLock()
	state := s.updateState
	s.settingsMu.RUnlock()

	token := ""
	if updated.GitHubToken != "" {
		token = maskToken(updated.GitHubToken)
	}

	return updateSettingsResponse{
		CheckIntervalHours:  updated.CheckIntervalHours,
		AutoUpdatePanel:     updated.AutoUpdatePanel,
		AutoUpdateAgents:    updated.AutoUpdateAgents,
		GitHubRepo:          updated.GitHubRepo,
		GitHubToken:         token,
		AgentDownloadSource: updated.AgentDownloadSource,
		CurrentVersion:      s.version,
		State:               state,
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

		// Detached from r.Context() on purpose: the admin-triggered check must
		// outlive the HTTP request, otherwise closing the browser tab would
		// abort the poll. The package-level ctx is not reachable from the
		// handler so we start fresh; the worker honours its own deadlines via
		// the timeout in checkForUpdates -> FetchLatestVersions. N-1: tracked
		// in bgWG so a graceful Shutdown waits for it to finish.
		s.bgWG.Add(1)
		//nolint:contextcheck,gosec // intentionally detached from request lifecycle
		go func() {
			defer s.bgWG.Done()
			s.checkForUpdates(context.Background())
		}()

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

		targetVersion, ok := s.resolvePanelTargetVersion(w, req.TargetVersion, state)
		if !ok {
			return
		}

		downloadURL, checksumURL, signatureURL, ok := s.resolvePanelDownloadAssets(w, r, targetVersion, state, settings)
		if !ok {
			return
		}

		writeJSON(w, http.StatusAccepted, panelUpdateResponse{
			Status: "updating",
			From:   s.version,
			To:     targetVersion,
		})

		// Detached from r.Context() on purpose: the panel-update goroutine
		// downloads, verifies, and replaces the running binary; killing it
		// when the operator's HTTP request ends would leave the panel in a
		// half-applied state. The 202 response above already tells the
		// caller the work continues asynchronously. N-1: tracked in bgWG so
		// shutdown waits for the binary swap to complete.
		s.bgWG.Add(1)
		//nolint:contextcheck,gosec // intentionally detached from request lifecycle
		go func() {
			defer s.bgWG.Done()
			s.performPanelUpdate(session.UserID, targetVersion, downloadURL, checksumURL, signatureURL, settings.GitHubToken)
		}()
	}
}

func (s *Server) resolvePanelTargetVersion(w http.ResponseWriter, requested string, state UpdateState) (string, bool) {
	targetVersion := strings.TrimSpace(requested)
	if targetVersion == "" {
		targetVersion = state.LatestPanelVersion
	}
	if targetVersion == "" {
		writeError(w, http.StatusBadRequest, "no target version specified and no latest version cached")
		return "", false
	}

	// Strip "v" prefix for comparison since UpdateState stores bare semver.
	currentVersion := strings.TrimPrefix(s.version, "v")
	if CompareVersions(targetVersion, currentVersion) <= 0 {
		writeError(w, http.StatusConflict, "target version is not newer than current version")
		return "", false
	}
	return targetVersion, true
}

func (s *Server) resolvePanelDownloadAssets(w http.ResponseWriter, r *http.Request, targetVersion string, state UpdateState, settings UpdateSettings) (string, string, string, bool) {
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
			return "", "", "", false
		}
		downloadURL, checksumURL, signatureURL = ResolveAssetURLs(panel, "control-plane")
	}

	if downloadURL == "" {
		writeError(w, http.StatusBadRequest, "no download URL available for target version")
		return "", "", "", false
	}
	if signatureURL == "" {
		// Refuse to install unsigned artifacts; the signature is the
		// primary defence against supply-chain tampering (SEC-02).
		writeError(w, http.StatusBadRequest, "release is missing a signature (.sig) asset; cannot install")
		return "", "", "", false
	}
	return downloadURL, checksumURL, signatureURL, true
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

	expectedChecksum, ok := s.fetchExpectedChecksum(ctx, checksumURL, token)
	if !ok {
		return
	}

	archivePath, ok := s.downloadAndVerifyPanelArchive(ctx, downloadURL, signatureURL, expectedChecksum, token)
	if !ok {
		return
	}
	// G703 false positive: archivePath is the temp file we just created
	// inside DownloadArchive (via os.MkdirTemp + filepath.Join), not a
	// caller-supplied path.
	defer func() { _ = os.Remove(archivePath) }() //nolint:gosec

	if !s.installPanelBinaryFromArchive(archivePath) {
		return
	}

	s.appendAuditWithContext(ctx, actorID, "panel.update.applied", "panel", map[string]any{
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

// fetchExpectedChecksum optionally fetches the published checksum.
// Returns ("", true) when no checksum URL was supplied.
func (s *Server) fetchExpectedChecksum(ctx context.Context, checksumURL, token string) (string, bool) {
	if checksumURL == "" {
		return "", true
	}
	checksum, err := DownloadChecksum(ctx, checksumURL, token)
	if err != nil {
		s.logger.Error("panel update: download checksum failed", "error", err)
		return "", false
	}
	return checksum, true
}

// downloadAndVerifyPanelArchive downloads the panel archive and verifies it
// against signature (mandatory) and checksum (optional). Returns the path on
// disk and true on success; the caller is responsible for removing the file.
func (s *Server) downloadAndVerifyPanelArchive(ctx context.Context, downloadURL, signatureURL, expectedChecksum, token string) (string, bool) {
	archivePath, err := DownloadArchive(ctx, downloadURL, token)
	if err != nil {
		s.logger.Error("panel update: download archive failed", "error", err)
		return "", false
	}
	if !s.verifyPanelArchive(ctx, archivePath, signatureURL, expectedChecksum, token) {
		// G703 false positive: archivePath comes from DownloadArchive's
		// internal os.MkdirTemp + filepath.Join, not from a request param.
		_ = os.Remove(archivePath) //nolint:gosec
		return "", false
	}
	return archivePath, true
}

// verifyPanelArchive runs signature verification and (if configured) checksum
// verification against an already-downloaded archive.
func (s *Server) verifyPanelArchive(ctx context.Context, archivePath, signatureURL, expectedChecksum, token string) bool {
	sig, err := DownloadSignature(ctx, signatureURL, token)
	if err != nil {
		s.logger.Error("panel update: download signature failed", "error", err)
		return false
	}
	archiveBytes, err := os.ReadFile(archivePath) //nolint:gosec // path created by DownloadArchive
	if err != nil {
		s.logger.Error("panel update: read archive for signature check failed", "error", err)
		return false
	}
	if err := security.VerifyArtifactBytes(archiveBytes, sig); err != nil {
		s.logger.Error("panel update: signature verification failed", "error", err)
		return false
	}
	if expectedChecksum != "" {
		if err := VerifyChecksum(archivePath, expectedChecksum); err != nil {
			s.logger.Error("panel update: checksum verification failed", "error", err)
			return false
		}
	}
	return true
}

// installPanelBinaryFromArchive extracts the binary from the verified archive
// and atomically replaces the running executable.
func (s *Server) installPanelBinaryFromArchive(archivePath string) bool {
	binaryPath, err := ExtractBinaryFromArchive(archivePath)
	if err != nil {
		s.logger.Error("panel update: extract binary failed", "error", err)
		return false
	}
	// G703 false positive: binaryPath is produced inside
	// ExtractBinaryFromArchive as os.MkdirTemp + filepath.Join, never a
	// caller-supplied path.
	defer func() { _ = os.Remove(binaryPath) }() //nolint:gosec

	currentBinary, err := os.Executable()
	if err != nil {
		s.logger.Error("panel update: resolve current binary path failed", "error", err)
		return false
	}

	if err := AtomicReplaceBinary(currentBinary, binaryPath); err != nil {
		s.logger.Error("panel update: atomic replace failed", "error", err)
		return false
	}
	return true
}

// agentUpdateAssets captures the URLs and target version resolved
// from the cached update state plus the operator's request body.
type agentUpdateAssets struct {
	targetVersion string
	downloadURL   string
	checksumURL   string
	signatureURL  string
}

// resolveAgentUpdateAssets validates the requested target version and
// returns the asset URLs from the cached update state. Returns
// (false, "", "") and writes the appropriate 4xx if anything is
// missing.
func resolveAgentUpdateAssets(w http.ResponseWriter, requestVersion string, state UpdateState) (agentUpdateAssets, bool) {
	targetVersion := requestVersion
	if targetVersion == "" {
		targetVersion = state.LatestAgentVersion
	}
	if targetVersion == "" {
		writeError(w, http.StatusBadRequest, "no agent version available")
		return agentUpdateAssets{}, false
	}
	if state.AgentDownloadURL == "" {
		writeError(w, http.StatusBadRequest, "no download URL available")
		return agentUpdateAssets{}, false
	}
	if state.AgentSignatureURL == "" {
		// Refuse to dispatch an unsigned agent update — the agent side
		// also refuses, but reject early so the operator sees the error
		// synchronously rather than via a failed job.
		writeError(w, http.StatusBadRequest, "release is missing a signature (.sig) asset; cannot update agent")
		return agentUpdateAssets{}, false
	}
	return agentUpdateAssets{
		targetVersion: targetVersion,
		downloadURL:   state.AgentDownloadURL,
		checksumURL:   state.AgentChecksumURL,
		signatureURL:  state.AgentSignatureURL,
	}, true
}

// buildAgentUpdatePayload assembles the JSON payload sent to the
// agent. When download_via_panel is set, the agent pulls the binary
// through the control-plane's proxy endpoint rather than directly
// from GitHub.
func (s *Server) buildAgentUpdatePayload(assets agentUpdateAssets, checksum string, settings UpdateSettings, requestHost string) []byte {
	downloadViaPanel := settings.AgentDownloadSource == "panel"
	payload := map[string]any{
		"version":            assets.targetVersion,
		"download_url":       assets.downloadURL,
		"checksum_sha256":    checksum,
		"signature_url":      assets.signatureURL,
		"download_via_panel": downloadViaPanel,
	}
	if downloadViaPanel {
		panelURL := s.panelSettings.HTTPPublicURL
		if panelURL == "" {
			panelURL = "http://" + requestHost
		}
		payload["panel_proxy_url"] = strings.TrimRight(panelURL, "/") +
			"/api/agent/update/binary?version=" + strings.TrimPrefix(assets.targetVersion, "v") +
			"&arch=amd64"
	}
	payloadJSON, _ := json.Marshal(payload)
	return payloadJSON
}

func (s *Server) handleAgentUpdate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := chi.URLParam(r, "id")

		agent, ok := s.lookupAgentForUpdate(w, agentID)
		if !ok {
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

		assets, ok := resolveAgentUpdateAssets(w, request.Version, state)
		if !ok {
			return
		}

		payloadJSON, ok := s.prepareAgentUpdatePayload(w, r, assets, settings)
		if !ok {
			return
		}

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

		job, err := s.jobs.Enqueue(r.Context(), jobs.CreateJobInput{
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
			"version": assets.targetVersion, "node_name": agent.NodeName,
		})

		writeJSON(w, http.StatusOK, map[string]any{
			"job_id":  job.ID,
			"status":  "dispatched",
			"version": assets.targetVersion,
		})
	}
}

// lookupAgentForUpdate returns the in-memory snapshot of the agent that the
// update is targeting, writing a 404 when no such agent is enrolled.
func (s *Server) lookupAgentForUpdate(w http.ResponseWriter, agentID string) (Agent, bool) {
	s.mu.RLock()
	agent, exists := s.agents[agentID]
	s.mu.RUnlock()
	if !exists {
		writeError(w, http.StatusNotFound, "agent not found")
		return Agent{}, false
	}
	return agent, true
}

// prepareAgentUpdatePayload downloads the GitHub-served checksum (with a
// per-call timeout) and builds the JSON payload the agent will receive in
// the update job. Returns ok=false after writing the appropriate HTTP error.
func (s *Server) prepareAgentUpdatePayload(w http.ResponseWriter, r *http.Request, assets agentUpdateAssets, settings UpdateSettings) ([]byte, bool) {
	checkCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	checksum, err := DownloadChecksum(checkCtx, assets.checksumURL, settings.GitHubToken)
	if err != nil {
		s.logger.Error("agent update: fetch checksum failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch checksum")
		return nil, false
	}
	return s.buildAgentUpdatePayload(assets, checksum, settings, r.Host), true
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
		version, arch, ok := parseAgentBinaryQuery(w, r)
		if !ok {
			return
		}

		s.settingsMu.RLock()
		settings := s.updateSettings
		s.settingsMu.RUnlock()

		rawURL, ok := buildAgentBinaryDownloadURL(w, settings, version, arch)
		if !ok {
			return
		}

		req, ok := buildAgentBinaryRequest(w, r, rawURL, settings.GitHubToken)
		if !ok {
			return
		}

		// secureDownloadClient restricts redirects to the GitHub allow-list so
		// a rogue release asset cannot steer us toward an attacker host.
		resp, err := secureDownloadClient().Do(req) //nolint:gosec // URL validated via validateUpdateHost + allow-list CheckRedirect
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

func parseAgentBinaryQuery(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	version := r.URL.Query().Get("version")
	arch := r.URL.Query().Get("arch")
	if version == "" || arch == "" {
		writeError(w, http.StatusBadRequest, "version and arch query parameters required")
		return "", "", false
	}
	if _, ok := allowedAgentArches[arch]; !ok {
		writeError(w, http.StatusBadRequest, "unsupported arch; allowed: amd64, arm64")
		return "", "", false
	}
	return version, arch, true
}

func buildAgentBinaryDownloadURL(w http.ResponseWriter, settings UpdateSettings, version, arch string) (string, bool) {
	// Re-validate the stored repo before interpolating it into the URL,
	// in case an earlier code path bypassed handlePutUpdateSettings' check.
	if err := validateGitHubRepo(settings.GitHubRepo); err != nil {
		writeError(w, http.StatusBadRequest, "invalid github_repo configured")
		return "", false
	}

	assetName := fmt.Sprintf("panvex-agent-linux-%s", arch)
	rawURL := fmt.Sprintf("https://github.com/%s/releases/download/agent/v%s/%s",
		settings.GitHubRepo, strings.TrimPrefix(version, "v"), assetName)
	if err := checkDownloadURL(rawURL); err != nil {
		writeError(w, http.StatusBadRequest, "invalid download URL")
		return "", false
	}
	return rawURL, true
}

func buildAgentBinaryRequest(w http.ResponseWriter, r *http.Request, rawURL, token string) (*http.Request, bool) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, rawURL, nil) //nolint:gosec // URL validated via validateUpdateHost + allow-list CheckRedirect
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create request")
		return nil, false
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/octet-stream")
	return req, true
}
