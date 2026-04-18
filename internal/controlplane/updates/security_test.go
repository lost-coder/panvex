package updates

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateGitHubRepo_Valid(t *testing.T) {
	cases := []string{
		"lost-coder/panvex",
		"o/r",
		"owner/repo.name",
		"a-b-c/d_e.f",
		"Owner123/Repo456",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if err := ValidateGitHubRepo(c); err != nil {
				t.Fatalf("ValidateGitHubRepo(%q) error = %v, want nil", c, err)
			}
		})
	}
}

func TestValidateGitHubRepo_Invalid(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"just-owner",
		"/repo",
		"owner/",
		"owner//repo",
		"owner/repo/extra",
		"../evil/../traversal/../x",
		"owner with space/repo",
		"owner/repo with space",
		"owner/repo?query",
		"owner/repo#fragment",
		"-startsWithHyphen/repo",
		"owner/.startsWithDot",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if err := ValidateGitHubRepo(c); err == nil {
				t.Fatalf("ValidateGitHubRepo(%q) error = nil, want failure", c)
			}
		})
	}
}

func TestCheckDownloadURL_Allowed(t *testing.T) {
	urls := []string{
		"https://github.com/lost-coder/panvex/releases/download/control-plane/v1.0.0/panvex-control-plane-linux-amd64.tar.gz",
		"https://api.github.com/repos/lost-coder/panvex/releases",
		"https://objects.githubusercontent.com/path/to/asset",
		"https://codeload.github.com/lost-coder/panvex/archive/refs/tags/v1.0.tar.gz",
	}
	for _, u := range urls {
		if err := CheckDownloadURL(u); err != nil {
			t.Fatalf("CheckDownloadURL(%q) error = %v, want nil", u, err)
		}
	}
}

func TestCheckDownloadURL_Rejected(t *testing.T) {
	cases := []string{
		"http://github.com/foo/bar",         // plaintext http
		"https://attacker.com/evil",         // unknown host
		"https://github.com.attacker.com/x", // host confusion
		"https://example.github.com/x",      // subdomain not in list
		"ftp://github.com/foo",              // wrong scheme
		"https://",                          // no host
		"https:// space.com",                // malformed
		"not a url",                         // unparseable
	}
	for _, u := range cases {
		if err := CheckDownloadURL(u); err == nil {
			t.Fatalf("CheckDownloadURL(%q) error = nil, want failure", u)
		}
	}
}

func TestSecureDownloadClient_RejectsExternalRedirect(t *testing.T) {
	// Origin server responds with a 302 that points at a host NOT in the allow-list;
	// the secure client's CheckRedirect must abort the request rather than follow.
	external := "https://attacker.example.com/exfil"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external, http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	// Exercise RestrictedRedirectPolicy in isolation: the real SecureDownloadClient
	// would reject the origin URL itself (httptest server is not on the allow-list),
	// so we use a bare client with only the CheckRedirect wired in.
	client := &http.Client{CheckRedirect: RestrictedRedirectPolicy()}
	req, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest error = %v", err)
	}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("client.Do error = nil, want failure for external redirect")
	}
	if !strings.Contains(err.Error(), "not in the allow-list") {
		t.Fatalf("unexpected error = %v", err)
	}
}
