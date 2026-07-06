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

// GitHubRelease re-exported for handlers/tests that still reference the
// short name through this package.
type (
	GitHubRelease = updates.GitHubRelease
	GitHubAsset   = updates.GitHubReleaseAsset
)

// ParseReleaseTag delegates to updates.ParseReleaseTag.
func ParseReleaseTag(tag string) (component, version string, ok bool) {
	return updates.ParseReleaseTag(tag)
}

// CompareVersions delegates to updates.CompareVersions.
func CompareVersions(a, b string) int { return updates.CompareVersions(a, b) }

// FetchLatestVersions delegates to updates.FetchLatestVersions.
func FetchLatestVersions(ctx context.Context, repo, token string) (panel, agent *GitHubRelease, err error) {
	return updates.FetchLatestVersions(ctx, repo, token)
}

// ResolveAssetURLs delegates to updates.ResolveAssetURLs.
func ResolveAssetURLs(release *GitHubRelease, component string) (binaryURL, checksumURL string) {
	return updates.ResolveAssetURLs(release, component)
}

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
// persists the result into s.updateState.
func (s *Server) checkForUpdates(ctx context.Context) {
	s.settingsMu.RLock()
	repo := s.updateSettings.GitHubRepo
	token := s.updateSettings.GitHubToken
	s.settingsMu.RUnlock()

	if repo == "" {
		return
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	panel, agent, err := FetchLatestVersions(fetchCtx, repo, token)
	if err != nil {
		s.logger.WarnContext(ctx, "update check failed", "error", err)
		s.recordUpdateCheckError(ctx, err)
		return
	}

	state := UpdateState{
		LastCheckedAt: s.now().Unix(),
	}

	if panel != nil {
		_, version, _ := ParseReleaseTag(panel.TagName)
		binaryURL, checksumURL := ResolveAssetURLs(panel, "control-plane")
		state.LatestPanelVersion = version
		state.PanelDownloadURL = binaryURL
		state.PanelChecksumURL = checksumURL
		state.PanelChangelog = panel.Body
	}

	if agent != nil {
		// The agent resolves its own per-arch asset URLs at update time, so
		// the panel only needs the version + changelog here.
		_, version, _ := ParseReleaseTag(agent.TagName)
		state.LatestAgentVersion = version
		state.AgentChangelog = agent.Body
	}

	// A successful check clears any prior error (LastCheckError defaults to "").
	s.settingsMu.Lock()
	s.updateState = state
	s.settingsMu.Unlock()

	s.persistUpdateState(ctx, state)

	s.logger.InfoContext(ctx, "update check completed",
		"panel_version", state.LatestPanelVersion,
		"agent_version", state.LatestAgentVersion,
	)
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
