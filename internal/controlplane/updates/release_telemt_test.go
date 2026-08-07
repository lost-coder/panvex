package updates

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// TestPickLatestTelemtRelease_IgnoresListOrderPicksSemverMax is the
// regression guard for the review finding: GitHub's releases API returns
// entries in publish-date order, NOT semver order. A hotfix published later
// on an older branch (3.3.9, listed first / newest-published) must lose to a
// numerically newer release that happened to ship earlier (3.4.25).
func TestPickLatestTelemtRelease_IgnoresListOrderPicksSemverMax(t *testing.T) {
	releases := []GitHubRelease{
		{TagName: "3.3.9"},  // published most recently, but semver-oldest of the three
		{TagName: "3.4.25"}, // the true semver max
		{TagName: "3.4.1"},
	}
	release := pickLatestTelemtRelease(releases)
	if release == nil {
		t.Fatal("pickLatestTelemtRelease() = nil, want 3.4.25")
	}
	if release.TagName != "3.4.25" {
		t.Fatalf("pickLatestTelemtRelease().TagName = %q, want %q (semver max, not list-order first)", release.TagName, "3.4.25")
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
	ApplyTelemtCheckResult(&state, nil, nil, errGithubRateLimit(t))

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

	ApplyTelemtCheckResult(&state, release, nil, nil)

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

func TestPickTopTelemtVersions(t *testing.T) {
	releases := []GitHubRelease{
		{TagName: "3.4.24"},                   // stable, старее
		{TagName: "3.5.0-rc1"},                // prerelease-тег — отбрасывается паттерном
		{TagName: "3.4.25"},                   // stable, новее (порядок в списке НЕ по semver)
		{TagName: "3.4.23", Draft: true},      // draft — отбрасывается
		{TagName: "3.4.22", Prerelease: true}, // prerelease-флаг — отбрасывается
		{TagName: "3.4.21"},
		{TagName: "3.4.20"},
	}
	got := pickTopTelemtVersions(releases, 3)
	want := []string{"3.4.25", "3.4.24", "3.4.21"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pickTopTelemtVersions = %v, want %v", got, want)
	}
	// n больше числа кандидатов — вернуть всех, без паники
	if got := pickTopTelemtVersions(releases, 99); len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	// ноль кандидатов — пустой НЕ-nil срез
	if got := pickTopTelemtVersions(nil, 5); got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice, got %#v", got)
	}
}

func TestApplyTelemtCheckResult_SetsAvailableVersions(t *testing.T) {
	state := State{}
	ApplyTelemtCheckResult(&state, &GitHubRelease{TagName: "3.4.25"}, []string{"3.4.25", "3.4.24"}, nil)
	if !reflect.DeepEqual(state.TelemtAvailableVersions, []string{"3.4.25", "3.4.24"}) {
		t.Fatalf("TelemtAvailableVersions = %v", state.TelemtAvailableVersions)
	}
	// успех с nil versions → пустой не-nil срез (JSON-инвариант: [] а не null)
	ApplyTelemtCheckResult(&state, &GitHubRelease{TagName: "3.4.25"}, nil, nil)
	if state.TelemtAvailableVersions == nil {
		t.Fatal("TelemtAvailableVersions = nil, want empty slice")
	}
}

func TestApplyTelemtCheckResult_ErrorPreservesAvailableVersions(t *testing.T) {
	state := State{TelemtAvailableVersions: []string{"3.4.25", "3.4.24"}}
	ApplyTelemtCheckResult(&state, nil, nil, errors.New("rate limit"))
	if !reflect.DeepEqual(state.TelemtAvailableVersions, []string{"3.4.25", "3.4.24"}) {
		t.Fatalf("versions not preserved on error: %v", state.TelemtAvailableVersions)
	}
}

// TestClampTelemtVersionsToShow pins the Task 2 clamp contract used both by
// mergeUpdateSettings (server, on PUT) and Service.LoadSettings (defensive
// normalization of a legacy persisted blob without the field, which
// unmarshals as 0): non-positive falls back to the default, above 20 is
// capped at 20, everything else passes through unchanged.
func TestClampTelemtVersionsToShow(t *testing.T) {
	cases := map[int]int{0: 5, -3: 5, 1: 1, 5: 5, 20: 20, 21: 20, 100: 20}
	for in, want := range cases {
		if got := ClampTelemtVersionsToShow(in); got != want {
			t.Fatalf("Clamp(%d) = %d, want %d", in, got, want)
		}
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
