package server

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// Task P3-ARCH-01d: the pure release-discovery helpers + DTOs now live
// in controlplane/updates. The *Server-bound worker below still owns
// orchestration because it reads s.updateSettings, writes s.updateState
// through s.settingsMu, persists via s.updatesSvc, and logs through
// s.logger. Keeping it here is deliberate: the task is an
// incremental split, not a full extraction.

// startUpdateCheckerWorker launches a background goroutine that periodically
// polls GitHub for new releases and updates s.updateState.
func (s *Server) startUpdateCheckerWorker(ctx context.Context) {
	s.settingsMu.RLock()
	interval := time.Duration(s.updateSettings.CheckIntervalHours) * time.Hour
	s.settingsMu.RUnlock()

	if interval <= 0 {
		return
	}

	s.rollupWg.Add(1)
	go func() {
		defer s.rollupWg.Done()
		// Initial check after a short delay to avoid slowing startup.
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
			s.checkForUpdates(ctx)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkForUpdates(ctx)
			}
		}
	}()
}

// checkForUpdates fetches the latest release information from GitHub and
// persists the result into s.updateState. The panel/agent check and the
// Telemt check are independent tries within the same tick — each owns a
// disjoint set of fields on State, persists on its own, and gets its own
// fetch deadline (see checkPanelAndAgentUpdates / checkTelemtRelease), so a
// failure OR a slow response in one never blocks, starves, or corrupts the
// other (Task 10, Telemt Update v1).
func (s *Server) checkForUpdates(ctx context.Context) {
	s.settingsMu.RLock()
	repo := s.updateSettings.GitHubRepo
	token := s.updateSettings.GitHubToken
	telemtVersionsToShow := s.updateSettings.TelemtVersionsToShow
	s.settingsMu.RUnlock()

	if repo != "" {
		s.checkPanelAndAgentUpdates(ctx, repo, token)
	}
	s.checkTelemtRelease(ctx, token, telemtVersionsToShow)
}

// updateCheckFetchTimeout bounds a single release-check's GitHub round trip.
// checkPanelAndAgentUpdates and checkTelemtRelease each derive their own
// timeout from this constant off the tick's parent context, rather than
// sharing one deadline, so a slow panel/agent fetch cannot eat into the
// Telemt check's budget (or vice versa).
const updateCheckFetchTimeout = 30 * time.Second

// checkPanelAndAgentUpdates queries the operator-configured GitHub repo for
// the latest control-plane and agent releases and persists the result into
// s.updateState. Only the panel/agent-owned fields are touched — Telemt's
// fields on the same State are read-modify-written from the live
// s.updateState, so a same-tick Telemt result (run before or after this call)
// is never clobbered.
func (s *Server) checkPanelAndAgentUpdates(ctx context.Context, repo, token string) {
	fetchCtx, cancel := context.WithTimeout(ctx, updateCheckFetchTimeout)
	defer cancel()

	panel, agent, err := updates.FetchLatestVersions(fetchCtx, repo, token)
	if err != nil {
		s.logger.WarnContext(ctx, "update check failed", "error", err)
		s.recordUpdateCheckError(ctx, err)
		return
	}

	s.settingsMu.Lock()
	state := s.updateState
	state.LastCheckedAt = s.now().Unix()
	// A successful check clears any prior error and resets the panel/agent
	// fields before conditionally repopulating them below — a release page
	// that no longer lists a component drops its stale version, matching the
	// pre-Task-10 behavior for these fields.
	state.LastCheckError = ""
	state.LatestPanelVersion = ""
	state.PanelDownloadURL = ""
	state.PanelChecksumURL = ""
	state.PanelChangelog = ""
	state.LatestAgentVersion = ""
	state.AgentChangelog = ""

	if panel != nil {
		_, version, _ := updates.ParseReleaseTag(panel.TagName)
		binaryURL, checksumURL := updates.ResolveAssetURLs(panel, "control-plane")
		state.LatestPanelVersion = version
		state.PanelDownloadURL = binaryURL
		state.PanelChecksumURL = checksumURL
		state.PanelChangelog = panel.Body
	}

	if agent != nil {
		// The agent resolves its own per-arch asset URLs at update time, so
		// the panel only needs the version + changelog here.
		_, version, _ := updates.ParseReleaseTag(agent.TagName)
		state.LatestAgentVersion = version
		state.AgentChangelog = agent.Body
	}

	s.updateState = state
	s.settingsMu.Unlock()

	s.persistUpdateState(ctx, state)

	s.logger.InfoContext(ctx, "update check completed",
		"panel_version", state.LatestPanelVersion,
		"agent_version", state.LatestAgentVersion,
	)
}

// checkTelemtRelease queries the fixed upstream telemt/telemt repository for
// the newest stable release and merges the result into s.updateState. It is
// its own try within the tick — see checkForUpdates — and only ever touches
// the Telemt-owned fields on State (TelemtLatestVersion /
// TelemtReleaseBaseURL / TelemtLastCheckError / TelemtAvailableVersions),
// reading the current s.updateState as its base so the panel/agent result
// from earlier in the same tick is never clobbered.
//
// versionsToShow is the operator-configured Settings.TelemtVersionsToShow,
// read by the caller under s.settingsMu alongside token. It is re-clamped
// here (not just trusted from the caller) so a settings blob persisted
// before this field existed — which unmarshals to 0 — never asks GitHub for
// a zero-length top-N instead of the default.
func (s *Server) checkTelemtRelease(ctx context.Context, token string, versionsToShow int) {
	fetchCtx, cancel := context.WithTimeout(ctx, updateCheckFetchTimeout)
	defer cancel()

	release, versions, err := updates.FetchTelemtReleaseOverview(fetchCtx, token, updates.ClampTelemtVersionsToShow(versionsToShow))
	if err != nil {
		s.logger.WarnContext(ctx, "telemt update check failed", "error", err)
	}

	s.settingsMu.Lock()
	state := s.updateState
	updates.ApplyTelemtCheckResult(&state, release, versions, err)
	state.LastCheckedAt = s.now().Unix()
	s.updateState = state
	s.settingsMu.Unlock()

	s.persistUpdateState(ctx, state)

	if err == nil {
		s.logger.InfoContext(ctx, "telemt update check completed",
			"telemt_version", state.TelemtLatestVersion,
		)
	}
}

// recordUpdateCheckError stores the reason the latest check failed so the
// dashboard can show it. Previously-known versions are preserved — only the
// error and the timestamp are updated.
func (s *Server) recordUpdateCheckError(ctx context.Context, checkErr error) {
	s.settingsMu.Lock()
	s.updateState.LastCheckError = checkErr.Error()
	s.updateState.LastCheckedAt = s.now().Unix()
	state := s.updateState
	s.settingsMu.Unlock()

	s.persistUpdateState(ctx, state)
}

// persistUpdateState writes the cached update state to the store, if present.
func (s *Server) persistUpdateState(ctx context.Context, state UpdateState) {
	if s.updatesSvc == nil {
		return
	}
	if err := s.updatesSvc.SaveState(ctx, state); err != nil {
		s.logger.ErrorContext(ctx, "persist update state failed", "error", err)
	}
}
