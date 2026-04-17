package server

import (
	"net/http"
	"net/url"
	"strings"
)

// csrfExemptAPISuffixes lists the API-relative path suffixes (after the
// configured panel or agent root-path prefix) that are exempt from the
// Origin-header CSRF check. These endpoints are agent-initiated (driven by
// the Panvex agent process, not a browser) and therefore do not carry a
// browser Origin header. They remain protected by other mechanisms:
//   - bootstrap tokens (Bearer) for /agent/bootstrap
//   - agent certificate recovery grants for /agent/recover-certificate
//
// Matching is performed against the explicit set of legal full paths
// constructed from the configured panel and agent root-paths. A bare
// /api/agent/... prefix (empty root-path) is always a valid exempt form.
// Arbitrary attacker-controlled prefixes such as /attacker/api/agent/bootstrap
// are NOT exempt.
var csrfExemptAPISuffixes = []string{
	"/api/agent/bootstrap",
	"/api/agent/recover-certificate",
}

// securityHeaders applies standard security response headers to every HTTP response.
//
// Content-Security-Policy:
//   - script-src 'self' only: no 'unsafe-inline'. All scripts must be loaded
//     as external files (Vite production build emits external ES modules).
//     Runtime configuration such as the UI root path is carried via the
//     data-root-path attribute on <html> (see serveUIIndex) and read via
//     document.documentElement.dataset.rootPath, not through inline scripts.
//   - style-src 'self' only: Tailwind v4 emits an external CSS bundle, so
//     inline style attributes on elements are no longer required to render
//     the app. (Radix UI and some component libraries occasionally use inline
//     style attributes; those are governed by the HTML style="..." attribute
//     and are allowed regardless of style-src policy under CSP Level 3.)
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' https://fonts.googleapis.com; connect-src 'self' wss:; img-src 'self' data:; font-src 'self' https://fonts.gstatic.com; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// csrfOriginCheck rejects state-changing requests (POST, PUT, DELETE, PATCH)
// whose Origin header is missing or does not match the request host. This
// prevents cross-site request forgery for all state-changing APIs, including
// those fronted by cookies and those fronted by bearer tokens or other
// browser-accessible credentials.
//
// Safe methods (GET, HEAD, OPTIONS) pass through unmodified.
//
// Agent endpoints under the configured panel or agent root-path are exempt
// because they are driven by the agent process (no browser, no Origin
// header). They are authenticated by bootstrap tokens or certificate
// recovery grants.
//
// Referer is intentionally NOT consulted as a fallback: it is suppressable
// via Referrer-Policy and unreliable as a CSRF signal.
//
// panelRootPath and agentRootPath are the configured HTTP root-path prefixes
// (may be empty). They are compared exactly — attacker-controlled prefixes
// such as "/attacker/api/agent/bootstrap" do not match.
func csrfOriginCheck(panelRootPath, agentRootPath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			if isCSRFExemptPath(r.URL.Path, panelRootPath, agentRootPath) {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin == "" {
				writeError(w, http.StatusForbidden, "missing origin header for state-changing request")
				return
			}

			if !originMatchesHost(origin, r.Host) {
				writeError(w, http.StatusForbidden, "cross-origin request blocked")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isCSRFExemptPath reports whether the given request path is an agent
// endpoint that must not require a browser Origin header. Exempt paths
// are constructed as <rootPath> + <csrfExemptAPISuffixes[i]> for each
// configured root path (panel and agent) plus the empty root-path form.
// Matching is an exact string comparison — prefixes outside the configured
// root-paths (e.g. "/attacker/api/agent/bootstrap") are NOT exempt.
func isCSRFExemptPath(requestPath, panelRootPath, agentRootPath string) bool {
	// The empty root-path is always a valid form: agent endpoints are
	// registered at the bare /api/... when no root-path is configured.
	roots := []string{""}
	if panelRootPath != "" {
		roots = append(roots, panelRootPath)
	}
	if agentRootPath != "" && agentRootPath != panelRootPath {
		roots = append(roots, agentRootPath)
	}
	for _, root := range roots {
		for _, suffix := range csrfExemptAPISuffixes {
			if requestPath == root+suffix {
				return true
			}
		}
	}
	return false
}

// originMatchesHost checks whether the host portion of the origin URL matches
// the request host. Port differences are tolerated when the origin uses a default
// port (80/443) that the browser omits.
func originMatchesHost(origin string, requestHost string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}

	originHost := parsed.Host
	if originHost == "" {
		originHost = parsed.Path
	}

	// Strip default ports for comparison.
	requestHost = stripDefaultPort(requestHost)
	originHost = stripDefaultPort(originHost)

	return strings.EqualFold(originHost, requestHost)
}

func stripDefaultPort(host string) string {
	if strings.HasSuffix(host, ":80") || strings.HasSuffix(host, ":443") {
		idx := strings.LastIndex(host, ":")
		return host[:idx]
	}
	return host
}
