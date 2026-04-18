package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
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
func FetchLatestVersions(ctx context.Context, repo, token string) (panel, agent *GitHubRelease, err error) {
	if vErr := ValidateGitHubRepo(repo); vErr != nil {
		return nil, nil, fmt.Errorf("fetch latest versions: %w", vErr)
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=20", repo)
	if uErr := CheckDownloadURL(url); uErr != nil {
		return nil, nil, fmt.Errorf("fetch latest versions: %w", uErr)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := SecureDownloadClient().Do(req)
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
