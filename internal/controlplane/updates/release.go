package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// GitHubRelease represents a single release from the GitHub Releases API.
type GitHubRelease struct {
	TagName     string              `json:"tag_name"`
	Body        string              `json:"body"`
	PublishedAt string              `json:"published_at"`
	Assets      []GitHubReleaseAsset `json:"assets"`
}

// GitHubReleaseAsset represents a downloadable asset attached to a GitHub
// release.
type GitHubReleaseAsset struct {
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

// CompareVersions performs a numeric semver comparison of two
// "major.minor.patch" version strings. It returns -1, 0, or 1.
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
// The repo argument must be a valid owner/repo slug; the resolved URL is
// rechecked against the GitHub allow-list before any network call.
func FetchLatestVersions(ctx context.Context, repo, token string) (*GitHubRelease, *GitHubRelease, error) {
	releases, err := fetchReleasesPage(ctx, repo, token)
	if err != nil {
		return nil, nil, err
	}
	panel, agent := pickLatestPanelAndAgent(releases)
	return panel, agent, nil
}

// fetchReleasesPage validates `repo`, builds an authenticated request,
// and decodes the first page of releases. Split out of
// FetchLatestVersions so the orchestration stays under the cognitive-
// complexity budget while preserving the same error wrapping.
func fetchReleasesPage(ctx context.Context, repo, token string) ([]GitHubRelease, error) {
	if vErr := ValidateGitHubRepo(repo); vErr != nil {
		return nil, fmt.Errorf("fetch latest versions: %w", vErr)
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=20", repo)
	if uErr := CheckDownloadURL(url); uErr != nil {
		return nil, fmt.Errorf("fetch latest versions: %w", uErr)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := SecureDownloadClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, githubAPIError(resp)
	}
	var releases []GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}
	return releases, nil
}

// githubAPIError turns a non-200 GitHub API response into an operator-readable
// error. The bare status code ("status 403") hides the cause; GitHub's JSON
// body and rate-limit headers carry it. For an exhausted rate limit it spells
// out the limit, the reset time, and the fix (add a token).
func githubAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	var parsed struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed)
	msg := strings.TrimSpace(parsed.Message)

	// Rate-limit exhaustion: GitHub returns 403 (or 429) with the remaining
	// count at 0. Unauthenticated requests are capped at 60/hour per IP.
	rateLimited := (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
		resp.Header.Get("X-RateLimit-Remaining") == "0"
	if rateLimited {
		limit := resp.Header.Get("X-RateLimit-Limit")
		if limit == "" {
			limit = "60"
		}
		detail := fmt.Sprintf("GitHub API rate limit exceeded (limit %s/hour)", limit)
		if reset := formatRateLimitReset(resp.Header.Get("X-RateLimit-Reset")); reset != "" {
			detail += "; resets at " + reset
		}
		detail += ". Add a GitHub token in update settings for a higher limit (5000/hour)."
		return errors.New(detail)
	}

	if msg != "" {
		return fmt.Errorf("github api returned status %d: %s", resp.StatusCode, msg)
	}
	return fmt.Errorf("github api returned status %d", resp.StatusCode)
}

// formatRateLimitReset renders the X-RateLimit-Reset epoch header as a UTC
// time string. Returns "" when the header is missing or unparseable.
func formatRateLimitReset(header string) string {
	if header == "" {
		return ""
	}
	secs, err := strconv.ParseInt(header, 10, 64)
	if err != nil {
		return ""
	}
	return time.Unix(secs, 0).UTC().Format("2006-01-02 15:04 UTC")
}

// pickLatestPanelAndAgent returns the first matching control-plane and
// agent release in the provided list. Releases are scanned in order so
// the GitHub API's newest-first ordering is preserved.
func pickLatestPanelAndAgent(releases []GitHubRelease) (*GitHubRelease, *GitHubRelease) {
	var panel, agent *GitHubRelease
	for i := range releases {
		component, _, ok := ParseReleaseTag(releases[i].TagName)
		if !ok {
			continue
		}
		switch {
		case component == "control-plane" && panel == nil:
			panel = &releases[i]
		case component == "agent" && agent == nil:
			agent = &releases[i]
		}
		if panel != nil && agent != nil {
			break
		}
	}
	return panel, agent
}

// ResolveAssetURLs finds the platform-specific binary, checksum, and
// signature download URLs for the given component from a GitHub release's
// assets. A missing signature URL is a fatal condition downstream — the
// update subsystem refuses to install unsigned artifacts.
func ResolveAssetURLs(release *GitHubRelease, component string) (binaryURL, checksumURL, signatureURL string) {
	if release == nil {
		return "", "", ""
	}
	arch := runtime.GOARCH
	archiveName := fmt.Sprintf("panvex-%s-linux-%s.tar.gz", component, arch)
	checksumName := archiveName + ".sha256"
	signatureName := archiveName + ".sig"

	for _, asset := range release.Assets {
		switch asset.Name {
		case archiveName:
			binaryURL = asset.BrowserDownloadURL
		case checksumName:
			checksumURL = asset.BrowserDownloadURL
		case signatureName:
			signatureURL = asset.BrowserDownloadURL
		}
	}
	return binaryURL, checksumURL, signatureURL
}
