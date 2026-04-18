package updates

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// gitHubRepoPattern matches valid GitHub `owner/repo` slugs. Owners are up to
// 39 chars (GitHub's documented cap) starting with alphanumerics; repo names
// must also start with an alphanumeric, then allow word chars, dot, hyphen up
// to 100 chars. Rejecting anything else prevents path traversal and URL
// injection when the value is interpolated into https://github.com/<repo>/...
// paths.
var gitHubRepoPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}/[a-zA-Z0-9][a-zA-Z0-9._-]{0,99}$`)

// ValidateGitHubRepo returns an error when s is not a valid owner/repo slug.
func ValidateGitHubRepo(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("github_repo cannot be empty")
	}
	if !gitHubRepoPattern.MatchString(s) {
		return fmt.Errorf("github_repo %q must match ^owner/repo^; only alphanumerics, '-', '.', '_' are allowed", s)
	}
	return nil
}

// allowedDownloadHosts lists the hostnames we trust to serve release artifacts.
// GitHub may 302 from github.com to objects.githubusercontent.com or
// codeload.github.com, so all three must be allowed. Any redirect whose final
// host is not in this set is refused to avoid exfiltration to attacker-controlled
// domains via a compromised/malicious repo setting.
var allowedDownloadHosts = map[string]struct{}{
	"github.com":                    {},
	"api.github.com":                {},
	"objects.githubusercontent.com": {},
	"codeload.github.com":           {},
}

// CheckDownloadURL rejects URLs whose scheme is not https or whose host is not
// in allowedDownloadHosts.
func CheckDownloadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("url %q: only https is allowed", raw)
	}
	host := strings.ToLower(u.Host)
	if _, ok := allowedDownloadHosts[host]; !ok {
		return fmt.Errorf("url %q: host %q is not in the allow-list", raw, host)
	}
	return nil
}

// RestrictedRedirectPolicy returns a CheckRedirect function that enforces
// CheckDownloadURL on every hop, so a rogue 302 cannot steer us off GitHub.
// It also caps the redirect chain at 10 hops to mirror http.Client defaults.
func RestrictedRedirectPolicy() func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return CheckDownloadURL(req.URL.String())
	}
}

// SecureDownloadClient returns an *http.Client with a redirect policy that
// confines requests to allowedDownloadHosts and a reasonable timeout for
// release-artifact fetches.
func SecureDownloadClient() *http.Client {
	return &http.Client{
		Timeout:       10 * time.Minute,
		CheckRedirect: RestrictedRedirectPolicy(),
	}
}
