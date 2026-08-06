package updates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// telemtReleasesServer starts an httptest server that serves releases as the
// GitHub Releases API would.
func telemtReleasesServer(t *testing.T, releases []GitHubRelease) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(releases); err != nil {
			t.Fatalf("encode releases: %v", err)
		}
	}))
}

// TestFetchAndPickLatestTelemtRelease_SkipsPrereleaseDraftAndPicksNewestStable
// covers the brief's first scenario: a page mixing a pre-release tag, a
// draft, and stable tags must resolve to the newest STABLE tag, not simply
// the first entry.
func TestFetchAndPickLatestTelemtRelease_SkipsPrereleaseDraftAndPicksNewestStable(t *testing.T) {
	releases := []GitHubRelease{
		{TagName: "3.5.0-rc1", Prerelease: true},
		{TagName: "3.4.26", Draft: true},
		{TagName: "3.4.25"},
		{TagName: "3.4.20"},
	}
	srv := telemtReleasesServer(t, releases)
	defer srv.Close()

	got, err := doFetchReleases(context.Background(), srv.URL+"/repos/telemt/telemt/releases?per_page=20", "")
	if err != nil {
		t.Fatalf("doFetchReleases() error = %v", err)
	}

	release := pickLatestTelemtRelease(got)
	if release == nil {
		t.Fatal("pickLatestTelemtRelease() = nil, want 3.4.25")
	}
	if release.TagName != "3.4.25" {
		t.Fatalf("pickLatestTelemtRelease().TagName = %q, want %q", release.TagName, "3.4.25")
	}
}

func TestPickLatestTelemtRelease_NoStableReleaseReturnsNil(t *testing.T) {
	releases := []GitHubRelease{
		{TagName: "3.5.0-rc1", Prerelease: true},
		{TagName: "3.4.26", Draft: true},
	}
	if got := pickLatestTelemtRelease(releases); got != nil {
		t.Fatalf("pickLatestTelemtRelease() = %+v, want nil", got)
	}
}

// TestFetchTelemtReleases_RateLimitError covers the brief's second scenario:
// a 403 rate-limit response must surface as an operator-readable error.
func TestFetchTelemtReleases_RateLimitError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Limit", "60")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded for 1.2.3.4."}`))
	}))
	defer srv.Close()

	_, err := doFetchReleases(context.Background(), srv.URL+"/repos/telemt/telemt/releases?per_page=20", "")
	if err == nil {
		t.Fatal("doFetchReleases() error = nil, want rate-limit error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "rate limit") {
		t.Fatalf("error = %q, want it to mention rate limit", err.Error())
	}
}

// TestApplyTelemtCheckResult_ErrorPreservesPreviousVersion covers the brief's
// second scenario end-to-end: a failed check must fill TelemtLastCheckError
// while leaving the previously known TelemtLatestVersion/TelemtReleaseBaseURL
// untouched.
func TestApplyTelemtCheckResult_ErrorPreservesPreviousVersion(t *testing.T) {
	state := State{
		TelemtLatestVersion:  "3.4.20",
		TelemtReleaseBaseURL: telemtReleaseBaseURL("3.4.20"),
	}
	ApplyTelemtCheckResult(&state, nil, errGithubRateLimit(t))

	if state.TelemtLatestVersion != "3.4.20" {
		t.Fatalf("TelemtLatestVersion = %q, want preserved %q", state.TelemtLatestVersion, "3.4.20")
	}
	if state.TelemtReleaseBaseURL != telemtReleaseBaseURL("3.4.20") {
		t.Fatalf("TelemtReleaseBaseURL = %q, want preserved", state.TelemtReleaseBaseURL)
	}
	if state.TelemtLastCheckError == "" {
		t.Fatal("TelemtLastCheckError = \"\", want the failure message")
	}
}

// TestApplyTelemtCheckResult_SuccessClearsError covers the brief's third
// scenario: a successful check clears any previously recorded error.
func TestApplyTelemtCheckResult_SuccessClearsError(t *testing.T) {
	state := State{TelemtLastCheckError: "GitHub API rate limit exceeded (limit 60/hour)"}
	release := &GitHubRelease{TagName: "3.4.25"}

	ApplyTelemtCheckResult(&state, release, nil)

	if state.TelemtLastCheckError != "" {
		t.Fatalf("TelemtLastCheckError = %q, want cleared", state.TelemtLastCheckError)
	}
	if state.TelemtLatestVersion != "3.4.25" {
		t.Fatalf("TelemtLatestVersion = %q, want %q", state.TelemtLatestVersion, "3.4.25")
	}
	wantURL := "https://github.com/telemt/telemt/releases/download/3.4.25"
	if state.TelemtReleaseBaseURL != wantURL {
		t.Fatalf("TelemtReleaseBaseURL = %q, want %q", state.TelemtReleaseBaseURL, wantURL)
	}
}

func errGithubRateLimit(t *testing.T) error {
	t.Helper()
	resp := respWith(http.StatusForbidden,
		`{"message":"API rate limit exceeded for 1.2.3.4."}`,
		map[string]string{
			"X-RateLimit-Remaining": "0",
			"X-RateLimit-Limit":     "60",
		})
	defer resp.Body.Close()
	return githubAPIError(resp)
}
