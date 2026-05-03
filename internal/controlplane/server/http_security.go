package server

import (
	"net/http"
	"net/url"
	"os"
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
// Content-Security-Policy (S-medium tightening):
//   - default-src 'none': no fallback. Every fetch destination must match
//     an explicit directive below; anything we forgot to allow-list is
//     blocked. Replaces the previous default-src 'self' which silently
//     allowed (e.g.) frame-src, media-src, prefetch-src, child-src.
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
//   - img-src 'self' data: blob:: data: covers Vite-inlined SVGs / tiny
//     icons; blob: covers any future canvas/object-URL preview.
//   - connect-src 'self' wss://<host> (S-08): WebSocket connections are scoped
//     to the request host so a script cannot redirect websockets to arbitrary
//     HTTPS endpoints. The Origin header and ws Origin-Pattern check enforce
//     this server-side too. The CSP is built per-request.
//   - font-src 'self' data:: @fontsource ships self-hosted woff2; data:
//     guards against future inline-fallback faces.
//   - manifest-src 'self', worker-src 'self' blob:: belt-and-suspenders.
//     The current build ships neither a web-app manifest nor service /
//     web workers; an attacker who manages to inject a <link rel="manifest">
//     or `new Worker(blobURL)` is held to this origin only.
//   - object-src 'none', base-uri 'none', frame-ancestors 'none': hard no.
//     base-uri 'none' (was 'self') prevents same-origin XSS from injecting
//     a <base href> that would re-anchor every relative URL on the page.
//   - form-action 'self' (M-15): login / recovery forms must POST same-origin.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// S-08: scope wss to request host so a script cannot redirect
		// websockets to arbitrary HTTPS endpoints. The Origin header
		// and ws Origin-Pattern check enforce this server-side too.
		wsOrigin := "wss://" + r.Host
		h.Set("Content-Security-Policy",
			"default-src 'none'; "+
				"script-src 'self'; "+
				"style-src 'self'; "+
				"img-src 'self' data: blob:; "+
				"connect-src 'self' "+wsOrigin+"; "+
				"font-src 'self' data:; "+
				"manifest-src 'self'; "+
				"worker-src 'self' blob:; "+
				"object-src 'none'; "+
				"base-uri 'none'; "+
				"form-action 'self'; "+
				"frame-ancestors 'none'")
		h.Set("Strict-Transport-Security", hstsHeaderValue())
		next.ServeHTTP(w, r)
	})
}

// hstsHeaderValue returns the Strict-Transport-Security value, optionally
// extended with `preload` and a 2-year max-age when PANVEX_HSTS_PRELOAD is set.
// Default (env unset) keeps the previous 1-year + includeSubDomains policy.
// (S-09)
func hstsHeaderValue() string {
	if hstsPreloadEnabled() {
		return "max-age=63072000; includeSubDomains; preload"
	}
	return "max-age=31536000; includeSubDomains"
}

func hstsPreloadEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PANVEX_HSTS_PRELOAD"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
func (s *Server) csrfOriginCheck(panelRootPath, agentRootPath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if msg, ok := s.csrfOriginReject(r, panelRootPath, agentRootPath); !ok {
				writeError(w, http.StatusForbidden, msg)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// csrfOriginReject returns ("", true) when the request is allowed and
// (msg, false) with the human-readable rejection reason otherwise. Split
// out of csrfOriginCheck so the middleware closure stays under the 15-CC
// limit (Sonar S3776).
func (s *Server) csrfOriginReject(r *http.Request, panelRootPath, agentRootPath string) (string, bool) {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return "", true
	}
	if isCSRFExemptPath(r.URL.Path, panelRootPath, agentRootPath) {
		return "", true
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return "missing origin header for state-changing request", false
	}
	if !originMatchesHost(origin, r.Host) {
		return "cross-origin request blocked", false
	}
	// Q3.U-S-21: also require the Origin scheme to match the request
	// scheme so an attacker on a downgraded http link cannot replay
	// calls to the https backend.
	if !originSchemeMatchesRequest(origin, s.trustedForwardedProto(r)) {
		return "origin scheme mismatch", false
	}
	return "", true
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
//
// Q3.U-S-21: a same-host origin still has to use a matching scheme — http
// origins are rejected when the request was forwarded over https (the
// X-Forwarded-Proto header) so an attacker on a downgraded connection
// cannot replay state-changing calls to the secure backend. https
// origins are always accepted; http origins only pass when the inbound
// request is also plain http.
func originMatchesHost(origin, requestHost string) bool {
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

// originSchemeMatchesRequest enforces the scheme half of Q3.U-S-21.
// requestProto is the trusted forwarded proto ("http" or "https") that
// the caller already resolved via trusted-proxy middleware. An origin
// whose scheme cannot be parsed is rejected.
func originSchemeMatchesRequest(origin, requestProto string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originScheme := strings.ToLower(parsed.Scheme)
	requestScheme := strings.ToLower(strings.TrimSpace(requestProto))
	if requestScheme == "" {
		// No proto signal available — accept either to avoid false
		// positives in dev/loopback setups that don't set
		// X-Forwarded-Proto. The host check still bounds the attack.
		return originScheme == "http" || originScheme == "https"
	}
	return originScheme == requestScheme
}

func stripDefaultPort(host string) string {
	if strings.HasSuffix(host, ":80") || strings.HasSuffix(host, ":443") {
		idx := strings.LastIndex(host, ":")
		return host[:idx]
	}
	return host
}
