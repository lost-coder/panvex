package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// Task P3-ARCH-01d: the pure release-discovery helpers + DTOs now live
// in controlplane/updates. The *Server-bound worker below still owns
// orchestration because it reads s.updateSettings, writes s.updateState
// through s.settingsMu, persists via s.store, and logs through
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
func ResolveAssetURLs(release *GitHubRelease, component string) (binaryURL, checksumURL, signatureURL string) {
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
		s.logger.Warn("update check failed", "error", err)
		return
	}

	state := UpdateState{
		LastCheckedAt: s.now().Unix(),
	}

	if panel != nil {
		_, version, _ := ParseReleaseTag(panel.TagName)
		binaryURL, checksumURL, signatureURL := ResolveAssetURLs(panel, "control-plane")
		state.LatestPanelVersion = version
		state.PanelDownloadURL = binaryURL
		state.PanelChecksumURL = checksumURL
		state.PanelSignatureURL = signatureURL
		state.PanelChangelog = panel.Body
	}

	if agent != nil {
		_, version, _ := ParseReleaseTag(agent.TagName)
		binaryURL, checksumURL, signatureURL := ResolveAssetURLs(agent, "agent")
		state.LatestAgentVersion = version
		state.AgentDownloadURL = binaryURL
		state.AgentChecksumURL = checksumURL
		state.AgentSignatureURL = signatureURL
		state.AgentChangelog = agent.Body
	}

	s.settingsMu.Lock()
	s.updateState = state
	s.settingsMu.Unlock()

	if s.store != nil {
		data, err := json.Marshal(state)
		if err == nil {
			if putErr := s.store.PutUpdateState(context.Background(), data); putErr != nil {
				s.logger.Error("persist update state failed", "error", putErr)
			}
		}
	}

	s.logger.Info("update check completed",
		"panel_version", state.LatestPanelVersion,
		"agent_version", state.LatestAgentVersion,
	)
}
