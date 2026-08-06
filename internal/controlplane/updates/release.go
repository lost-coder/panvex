package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// GitHubRelease represents a single release from the GitHub Releases API.
type GitHubRelease struct {
	TagName     string               `json:"tag_name"`
	Body        string               `json:"body"`
	PublishedAt string               `json:"published_at"`
	Draft       bool                 `json:"draft"`
	Prerelease  bool                 `json:"prerelease"`
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

// CompareVersions performs a semver comparison of two version strings
// ("1.2.3" or "v1.2.3"; pre-release suffixes follow semver ordering).
// It returns an error when either side is not a parseable semver —
// callers must fail closed (refuse the update) instead of comparing
// silently-zeroed segments: "1.x.3" used to compare equal to "1.0.3"
// (R1.5, audit 2026-07-07 §1.14).
func CompareVersions(a, b string) (int, error) {
	ca, ok := canonicalSemver(a)
	if !ok {
		return 0, fmt.Errorf("updates: version %q is not a parseable semver", a)
	}
	cb, ok := canonicalSemver(b)
	if !ok {
		return 0, fmt.Errorf("updates: version %q is not a parseable semver", b)
	}
	return semver.Compare(ca, cb), nil
}

// canonicalSemver normalises a version string into the leading-"v" form
// golang.org/x/mod/semver expects. Deliberate mirror of
// internal/agent/updater.canonicalSemver — the agent binary and the
// control-plane keep independent copies by design (no shared import in
// either direction); R6 (dedup) may consolidate into a leaf package.
func canonicalSemver(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", false
	}
	return v, true
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
	return doFetchReleases(ctx, url, token)
}

// doFetchReleases issues an authenticated GET against url and decodes the
// GitHub releases page. Split out of fetchReleasesPage so
// fetchTelemtReleasesPage below can share the same auth-header and
// rate-limit-error handling, and so tests can exercise the network/decode
// path directly against an httptest server without routing through the
// GitHub-only host allow-list (CheckDownloadURL stays at each production
// call site, above and in fetchTelemtReleasesPage).
func doFetchReleases(ctx context.Context, url, token string) ([]GitHubRelease, error) {
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

// ResolveAssetURLs finds the platform-specific binary and checksum download
// URLs for the given component from a GitHub release's assets. A missing
// checksum URL is a fatal condition downstream — the update subsystem refuses
// to install an artifact whose integrity it cannot verify.
func ResolveAssetURLs(release *GitHubRelease, component string) (binaryURL, checksumURL string) {
	if release == nil {
		return "", ""
	}
	arch := runtime.GOARCH
	archiveName := fmt.Sprintf("panvex-%s-linux-%s.tar.gz", component, arch)
	checksumName := archiveName + ".sha256"

	for _, asset := range release.Assets {
		switch asset.Name {
		case archiveName:
			binaryURL = asset.BrowserDownloadURL
		case checksumName:
			checksumURL = asset.BrowserDownloadURL
		}
	}
	return binaryURL, checksumURL
}

// telemtRepo is the fixed upstream Telemt project checked for new releases.
// Unlike Settings.GitHubRepo (operator-configurable, defaults to the panel's
// own fork), the Telemt release feed always points at the upstream repo — an
// operator cannot repoint it.
const telemtRepo = "telemt/telemt"

// telemtStableTagPattern matches Telemt's bare-semver release tags exactly:
// no "v" prefix, no pre-release suffix (see the release workflow's
// `[0-9]+.[0-9]+.[0-9]+` tag regex). A hyphenated tag such as "3.5.0-rc1" is
// a pre-release by construction and never matches.
var telemtStableTagPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// FetchLatestTelemtRelease queries the GitHub Releases API for telemtRepo and
// returns the newest stable release (not a draft, not flagged prerelease by
// GitHub, and tagged as a bare "X.Y.Z") found in the first page of results.
// Returns a nil release with a nil error when only pre-releases/drafts exist.
func FetchLatestTelemtRelease(ctx context.Context, token string) (*GitHubRelease, error) {
	releases, err := fetchTelemtReleasesPage(ctx, token)
	if err != nil {
		return nil, err
	}
	return pickLatestTelemtRelease(releases), nil
}

// fetchTelemtReleasesPage validates the resolved URL and delegates to
// doFetchReleases, mirroring fetchReleasesPage's validation step. telemtRepo
// is a fixed constant, so unlike fetchReleasesPage there is no operator input
// to run through ValidateGitHubRepo.
func fetchTelemtReleasesPage(ctx context.Context, token string) ([]GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=20", telemtRepo)
	if uErr := CheckDownloadURL(url); uErr != nil {
		return nil, fmt.Errorf("fetch latest telemt release: %w", uErr)
	}
	return doFetchReleases(ctx, url, token)
}

// pickLatestTelemtRelease returns the stable Telemt release with the highest
// semver tag among releases, skipping drafts, releases GitHub itself flags as
// pre-release, and any tag that is not a bare "X.Y.Z" (the hyphenated
// pre-release tag shape, e.g. "3.5.0-rc1"). It does NOT trust GitHub's
// newest-first list ordering (that's publish-date order, not semver order):
// a hotfix published later on an older branch would otherwise beat a
// numerically newer release that shipped earlier. telemtStableTagPattern
// already guarantees every candidate is a parseable bare semver, so
// CompareVersions is only defensively checked for an error.
func pickLatestTelemtRelease(releases []GitHubRelease) *GitHubRelease {
	var best *GitHubRelease
	for i := range releases {
		r := &releases[i]
		if r.Draft || r.Prerelease {
			continue
		}
		if !telemtStableTagPattern.MatchString(r.TagName) {
			continue
		}
		if best == nil {
			best = r
			continue
		}
		if cmp, err := CompareVersions(r.TagName, best.TagName); err == nil && cmp > 0 {
			best = r
		}
	}
	return best
}

// telemtReleaseBaseURL builds the download base URL for a Telemt release tag:
// https://github.com/telemt/telemt/releases/download/<tag> — the agent
// appends its own per-platform asset name to this when it self-updates.
func telemtReleaseBaseURL(tag string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s", telemtRepo, tag)
}

// ApplyTelemtCheckResult merges the outcome of a Telemt release check into
// state, following the "failure preserves, success clears" contract: an
// error leaves the previously known TelemtLatestVersion/TelemtReleaseBaseURL
// untouched and records the message so the dashboard can surface it, while a
// successful check always clears the previous error. A successful check that
// found no stable release (release == nil, err == nil) clears the error but
// otherwise leaves the previously known version/URL alone — the same "no
// signal, no change" behavior FetchLatestTelemtRelease's nil-nil return
// implies.
func ApplyTelemtCheckResult(state *State, release *GitHubRelease, err error) {
	if err != nil {
		state.TelemtLastCheckError = err.Error()
		return
	}
	state.TelemtLastCheckError = ""
	if release == nil {
		return
	}
	state.TelemtLatestVersion = release.TagName
	state.TelemtReleaseBaseURL = telemtReleaseBaseURL(release.TagName)
}
