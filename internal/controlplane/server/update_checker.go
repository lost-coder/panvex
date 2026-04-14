package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// GitHubRelease represents a single release from the GitHub Releases API.
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Body        string        `json:"body"`
	PublishedAt string        `json:"published_at"`
	Assets      []GitHubAsset `json:"assets"`
}

// GitHubAsset represents a downloadable asset attached to a GitHub release.
type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// ParseReleaseTag splits a GitHub release tag like "control-plane/v0.2.3"
// into a component name and a bare semver version string.
func ParseReleaseTag(tag string) (component, version string, ok bool) {
	idx := strings.Index(tag, "/")
	if idx <= 0 || idx >= len(tag)-1 {
		return "", "", false
	}
	component = tag[:idx]
	rest := tag[idx+1:]
	if !strings.HasPrefix(rest, "v") || len(rest) < 2 {
		return "", "", false
	}
	version = rest[1:]
	if version == "" {
		return "", "", false
	}
	return component, version, true
}

// CompareVersions performs a numeric semver comparison of two "major.minor.patch"
// version strings. It returns -1, 0, or 1.
func CompareVersions(a, b string) int {
	ap := parseSemverParts(a)
	bp := parseSemverParts(b)
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return 0
}

func parseSemverParts(v string) [3]int {
	var parts [3]int
	segments := strings.SplitN(v, ".", 3)
	for i, seg := range segments {
		if i >= 3 {
			break
		}
		n, _ := strconv.Atoi(seg)
		parts[i] = n
	}
	return parts
}

// FetchLatestVersions queries the GitHub Releases API and returns the newest
// control-plane and agent releases found in the first page of results.
// Either return value may be nil when no matching release is found.
func FetchLatestVersions(ctx context.Context, repo, token string) (panel, agent *GitHubRelease, err error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=20", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var releases []GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, nil, fmt.Errorf("decode releases: %w", err)
	}

	for i := range releases {
		component, _, ok := ParseReleaseTag(releases[i].TagName)
		if !ok {
			continue
		}
		if component == "control-plane" && panel == nil {
			panel = &releases[i]
		}
		if component == "agent" && agent == nil {
			agent = &releases[i]
		}
		if panel != nil && agent != nil {
			break
		}
	}

	return panel, agent, nil
}

// ResolveAssetURLs finds the platform-specific binary and checksum download
// URLs for the given component from a GitHub release's assets.
func ResolveAssetURLs(release *GitHubRelease, component string) (binaryURL, checksumURL string) {
	if release == nil {
		return "", ""
	}
	arch := runtime.GOARCH
	binaryName := fmt.Sprintf("panvex-%s-linux-%s", component, arch)
	checksumName := binaryName + ".sha256"

	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			binaryURL = asset.BrowserDownloadURL
		}
		if asset.Name == checksumName {
			checksumURL = asset.BrowserDownloadURL
		}
	}
	return binaryURL, checksumURL
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
		binaryURL, checksumURL := ResolveAssetURLs(panel, "control-plane")
		state.LatestPanelVersion = version
		state.PanelDownloadURL = binaryURL
		state.PanelChecksumURL = checksumURL
		state.PanelChangelog = panel.Body
	}

	if agent != nil {
		_, version, _ := ParseReleaseTag(agent.TagName)
		binaryURL, checksumURL := ResolveAssetURLs(agent, "agent")
		state.LatestAgentVersion = version
		state.AgentDownloadURL = binaryURL
		state.AgentChecksumURL = checksumURL
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
