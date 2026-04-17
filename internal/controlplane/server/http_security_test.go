package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newCSRFTestHandler builds a csrfOriginCheck middleware-wrapped handler with
// the provided root paths. Passing empty strings reproduces the plain
// /api/... registration mode used by most tests.
func newCSRFTestHandler(panelRootPath, agentRootPath string) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return csrfOriginCheck(panelRootPath, agentRootPath)(inner)
}

// TestCSRFOriginCheckBlocksStateChangingWithoutOrigin verifies that every
// state-changing method (POST/PUT/DELETE/PATCH) is rejected with 403 when
// no Origin header is present, regardless of whether a session cookie is
// attached. This is the P2-SEC-06 remediation: the previous cookie-
// conditional bypass is removed.
func TestCSRFOriginCheckBlocksStateChangingWithoutOrigin(t *testing.T) {
	handler := newCSRFTestHandler("", "")

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method+"_no_origin_no_cookie", func(t *testing.T) {
			req := httptest.NewRequest(method, "http://panel.example.com/api/jobs", bytes.NewReader([]byte(`{}`)))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s without Origin: status = %d, want %d", method, rec.Code, http.StatusForbidden)
			}
		})

		t.Run(method+"_no_origin_with_cookie", func(t *testing.T) {
			req := httptest.NewRequest(method, "http://panel.example.com/api/jobs", bytes.NewReader([]byte(`{}`)))
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "abc"})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s with cookie but without Origin: status = %d, want %d", method, rec.Code, http.StatusForbidden)
			}
		})
	}
}

// TestCSRFOriginCheckAllowsSafeMethods verifies GET/HEAD/OPTIONS pass
// through unmodified with or without Origin.
func TestCSRFOriginCheckAllowsSafeMethods(t *testing.T) {
	handler := newCSRFTestHandler("", "")

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "http://panel.example.com/api/fleet", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s without Origin: status = %d, want %d", method, rec.Code, http.StatusOK)
		}
	}
}

// TestCSRFOriginCheckAllowsMatchingOrigin verifies state-changing requests
// are accepted when Origin matches Host.
func TestCSRFOriginCheckAllowsMatchingOrigin(t *testing.T) {
	handler := newCSRFTestHandler("", "")

	req := httptest.NewRequest(http.MethodPost, "http://panel.example.com/api/jobs", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Origin", "http://panel.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestCSRFOriginCheckRejectsCrossOrigin verifies state-changing requests
// are rejected when Origin does not match Host.
func TestCSRFOriginCheckRejectsCrossOrigin(t *testing.T) {
	handler := newCSRFTestHandler("", "")

	req := httptest.NewRequest(http.MethodPost, "http://panel.example.com/api/jobs", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Origin", "http://evil.example.net")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// TestCSRFOriginCheckExemptsAgentEndpoints verifies that agent bootstrap
// and certificate-recovery endpoints are exempt from the Origin requirement
// at every supported path prefix (plain /api, panel rootPath/api, agent
// rootPath/api) when the corresponding root-path is configured.
func TestCSRFOriginCheckExemptsAgentEndpoints(t *testing.T) {
	// With both panel and agent root-paths configured, the middleware must
	// exempt /api/..., /panvex/api/..., and /agent/api/... forms.
	handler := newCSRFTestHandler("/panvex", "/agent")

	paths := []string{
		"/api/agent/bootstrap",
		"/api/agent/recover-certificate",
		"/panvex/api/agent/bootstrap",
		"/panvex/api/agent/recover-certificate",
		"/agent/api/agent/bootstrap",
		"/agent/api/agent/recover-certificate",
	}
	for _, p := range paths {
		req := httptest.NewRequest(http.MethodPost, "http://panel.example.com"+p, bytes.NewReader([]byte(`{}`)))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s without Origin: status = %d, want %d (must be exempt)", p, rec.Code, http.StatusOK)
		}
	}
}

// TestCSRFOriginCheckDoesNotExemptLookalikePaths verifies that attacker-
// controlled paths that merely contain the exempt suffix or use an
// unconfigured prefix are NOT exempt.
func TestCSRFOriginCheckDoesNotExemptLookalikePaths(t *testing.T) {
	handler := newCSRFTestHandler("/panvex", "/agent")

	// Each of these must be rejected with 403. No Origin header is sent.
	rejectedPaths := []string{
		// Substring-but-not-suffix — longer than any exempt suffix.
		"/api/agent/bootstrap/steal",
		// Attacker-shaped prefix: not equal to any configured root-path, so
		// even though it ends with the exempt suffix it must not be exempt.
		// This is the P2-SEC-06 review-finding regression guard.
		"/attacker/api/agent/bootstrap",
		"/attacker/api/agent/recover-certificate",
		// Nested attacker prefix in front of a legitimate root-path.
		"/evil/panvex/api/agent/bootstrap",
	}
	for _, p := range rejectedPaths {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://panel.example.com"+p, bytes.NewReader([]byte(`{}`)))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("attacker-shaped path %q must not be exempt: status = %d, want %d", p, rec.Code, http.StatusForbidden)
			}
		})
	}
}

// TestSecurityHeadersDoNotAllowInlineScripts verifies the Content-Security-
// Policy header no longer contains 'unsafe-inline' in script-src, and
// includes the additional object-src 'none' and base-uri 'self' hardening
// directives. This is the P2-SEC-09 remediation.
func TestSecurityHeadersDoNotAllowInlineScripts(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://panel.example.com/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header not set")
	}

	// Must contain an explicit script-src 'self' directive.
	if !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("CSP missing script-src 'self' directive: %q", csp)
	}

	// Must NOT allow 'unsafe-inline' in script-src. The directive structure
	// is simple (no nested punctuation), so we can detect any occurrence of
	// 'unsafe-inline' in the whole header; none is allowed for scripts.
	scriptSrc := extractDirective(csp, "script-src")
	if strings.Contains(scriptSrc, "'unsafe-inline'") {
		t.Fatalf("CSP script-src must not contain 'unsafe-inline': %q", scriptSrc)
	}
	if strings.Contains(scriptSrc, "'unsafe-eval'") {
		t.Fatalf("CSP script-src must not contain 'unsafe-eval': %q", scriptSrc)
	}

	// object-src 'none' and base-uri 'self' do NOT fall back to default-src,
	// so both must be explicitly present. These block <object>/<embed>/
	// <applet> plugin loads and prevent <base> tag injection from changing
	// relative-URL resolution.
	objectSrc := extractDirective(csp, "object-src")
	if objectSrc != "'none'" {
		t.Fatalf("CSP object-src must be 'none', got %q (full CSP: %q)", objectSrc, csp)
	}
	baseURI := extractDirective(csp, "base-uri")
	if baseURI != "'self'" {
		t.Fatalf("CSP base-uri must be 'self', got %q (full CSP: %q)", baseURI, csp)
	}

	// Verify other expected hardening headers are still present.
	for _, h := range []string{"X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy", "Permissions-Policy", "Strict-Transport-Security"} {
		if rec.Header().Get(h) == "" {
			t.Fatalf("expected security header %q to be set", h)
		}
	}
}

// extractDirective returns the token list for a single CSP directive,
// without the directive name.
func extractDirective(csp string, name string) string {
	for _, part := range strings.Split(csp, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, name+" ") || part == name {
			return strings.TrimSpace(strings.TrimPrefix(part, name))
		}
	}
	return ""
}

// TestIsCSRFExemptPath smoke-tests the helper directly. The helper takes
// explicit panel and agent root-paths and performs exact-match comparisons
// against <rootPath>+<suffix> forms (plus the bare suffix form).
func TestIsCSRFExemptPath(t *testing.T) {
	type testCase struct {
		path          string
		panelRootPath string
		agentRootPath string
		want          bool
	}

	cases := []testCase{
		// Bare /api/... form is always exempt — the empty root-path is
		// implicitly supported.
		{"/api/agent/bootstrap", "", "", true},
		{"/api/agent/recover-certificate", "", "", true},
		{"/api/agent/bootstrap", "/panvex", "/agent", true},
		{"/api/agent/recover-certificate", "/panvex", "/agent", true},

		// Configured panel/agent root-paths are exempt.
		{"/panvex/api/agent/bootstrap", "/panvex", "/agent", true},
		{"/agent/api/agent/recover-certificate", "/panvex", "/agent", true},

		// Non-agent endpoints are never exempt.
		{"/api/auth/login", "", "", false},
		{"/api/jobs", "", "", false},
		{"/", "", "", false},

		// Path with extra trailing segment is not exempt.
		{"/api/agent/bootstrap/extra", "", "", false},

		// Attacker-shaped prefixes that are not the configured root-path
		// must NOT be exempt (regression guard for the review finding).
		{"/attacker/api/agent/bootstrap", "", "", false},
		{"/attacker/api/agent/bootstrap", "/panvex", "/agent", false},
		{"/attacker/api/agent/recover-certificate", "/panvex", "/agent", false},

		// A configured panel root-path does NOT accidentally permit an
		// attacker to route through a different (unconfigured) prefix.
		{"/panvex/api/agent/bootstrap", "", "", false},
		{"/agent/api/agent/bootstrap", "", "", false},

		// Same root-path value for panel and agent — must only add one
		// entry (covered by dedup in isCSRFExemptPath) and still match.
		{"/panvex/api/agent/bootstrap", "/panvex", "/panvex", true},
	}

	for _, tc := range cases {
		got := isCSRFExemptPath(tc.path, tc.panelRootPath, tc.agentRootPath)
		if got != tc.want {
			t.Errorf("isCSRFExemptPath(%q, panel=%q, agent=%q) = %v, want %v",
				tc.path, tc.panelRootPath, tc.agentRootPath, got, tc.want)
		}
	}
}
