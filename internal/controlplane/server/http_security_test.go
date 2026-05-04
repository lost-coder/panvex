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
	srv := &Server{}
	return srv.csrfOriginCheck(panelRootPath, agentRootPath)(inner)
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
			req := httptest.NewRequestWithContext(t.Context(),method, "http://panel.example.com/api/jobs", bytes.NewReader([]byte(`{}`)))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s without Origin: status = %d, want %d", method, rec.Code, http.StatusForbidden)
			}
		})

		t.Run(method+"_no_origin_with_cookie", func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(),method, "http://panel.example.com/api/jobs", bytes.NewReader([]byte(`{}`)))
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
		req := httptest.NewRequestWithContext(t.Context(),method, "http://panel.example.com/api/fleet", nil)
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

	req := httptest.NewRequestWithContext(t.Context(),http.MethodPost, "http://panel.example.com/api/jobs", bytes.NewReader([]byte(`{}`)))
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

	req := httptest.NewRequestWithContext(t.Context(),http.MethodPost, "http://panel.example.com/api/jobs", bytes.NewReader([]byte(`{}`)))
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
		req := httptest.NewRequestWithContext(t.Context(),http.MethodPost, "http://panel.example.com"+p, bytes.NewReader([]byte(`{}`)))
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
			req := httptest.NewRequestWithContext(t.Context(),http.MethodPost, "http://panel.example.com"+p, bytes.NewReader([]byte(`{}`)))
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
// includes the default-src 'none' lockdown plus the explicit hardening
// directives (object-src 'none', base-uri 'none', frame-ancestors 'none').
// This combines the P2-SEC-09 remediation with the S-medium default-src
// lockdown: every fetch destination is explicitly allow-listed, with no
// fallback to default-src.
func TestSecurityHeadersDoNotAllowInlineScripts(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://panel.example.com/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header not set")
	}

	// default-src 'none': lockdown. Every fetch destination must match an
	// explicit directive below; nothing falls back to default-src. Catches
	// regressions that re-introduce 'self' (which silently allows e.g.
	// frame-src, media-src, prefetch-src, child-src).
	defaultSrc := extractDirective(csp, "default-src")
	if defaultSrc != "'none'" {
		t.Fatalf("CSP default-src must be 'none', got %q (full CSP: %q)", defaultSrc, csp)
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

	// object-src, base-uri, and frame-ancestors do NOT fall back to
	// default-src, so each must be explicitly present and locked to
	// 'none'. base-uri 'none' (was 'self') prevents injected <base href>
	// tags from rewriting relative URLs.
	for directive, want := range map[string]string{
		"object-src":      "'none'",
		"base-uri":        "'none'",
		"frame-ancestors": "'none'",
	} {
		got := extractDirective(csp, directive)
		if got != want {
			t.Fatalf("CSP %s must be %s, got %q (full CSP: %q)", directive, want, got, csp)
		}
	}

	// Explicit allow-list directives must be present so the default-src
	// 'none' lockdown does not break legitimate fetches.
	for _, directive := range []string{
		"script-src", "style-src", "img-src", "connect-src",
		"font-src", "manifest-src", "worker-src", "form-action",
	} {
		if extractDirective(csp, directive) == "" {
			t.Fatalf("CSP missing explicit %s directive (default-src is 'none'): %q", directive, csp)
		}
	}

	// Verify other expected hardening headers are still present.
	for _, h := range []string{"X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy", "Permissions-Policy", "Strict-Transport-Security"} {
		if rec.Header().Get(h) == "" {
			t.Fatalf("expected security header %q to be set", h)
		}
	}
}

// TestSecurityHeaders_CSPScopesWssToRequestHost verifies that the CSP
// connect-src directive contains a host-scoped wss:// origin rather than the
// unbounded wss: source (S-08).
func TestSecurityHeaders_CSPScopesWssToRequestHost(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://panel.example:8080/api/health", nil)
	req.Host = "panel.example:8080"
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)
	csp := rr.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "wss://panel.example:8080") {
		t.Fatalf("CSP missing scoped wss: %q", csp)
	}
	// The bare keyword "wss:" (not followed by //) must NOT appear.
	// We check the connect-src directive specifically to avoid matching
	// the scheme in the scoped wss:// value.
	connectSrc := extractDirective(csp, "connect-src")
	for _, token := range strings.Fields(connectSrc) {
		if token == "wss:" {
			t.Fatalf("CSP connect-src still has unbounded wss: keyword: %q", csp)
		}
	}
}

// TestHSTSHeader_DefaultIsOneYearWithoutPreload verifies that HSTS defaults
// to 1-year + includeSubDomains when PANVEX_HSTS_PRELOAD is unset (S-09).
func TestHSTSHeader_DefaultIsOneYearWithoutPreload(t *testing.T) {
	t.Setenv("PANVEX_HSTS_PRELOAD", "")
	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://panel.example/", nil)
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})).ServeHTTP(rr, req)
	got := rr.Header().Get("Strict-Transport-Security")
	if got != "max-age=31536000; includeSubDomains" {
		t.Fatalf("HSTS = %q, want default", got)
	}
}

// TestHSTSHeader_PreloadEnvOptIn verifies that HSTS uses a 2-year max-age
// with preload when PANVEX_HSTS_PRELOAD=1 (S-09).
func TestHSTSHeader_PreloadEnvOptIn(t *testing.T) {
	t.Setenv("PANVEX_HSTS_PRELOAD", "1")
	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://panel.example/", nil)
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})).ServeHTTP(rr, req)
	got := rr.Header().Get("Strict-Transport-Security")
	if got != "max-age=63072000; includeSubDomains; preload" {
		t.Fatalf("HSTS = %q, want preload form", got)
	}
}

// extractDirective returns the token list for a single CSP directive,
// without the directive name.
func extractDirective(csp, name string) string {
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

// TestIsCSRFExemptPath_RejectsAttackerControlledPrefix is a regression test
// for S-05. It locks down the exact-match semantics of isCSRFExemptPath and
// verifies that attacker-controlled prefixes (trailing slash, double-leading
// slash, path traversal, case folding, unconfigured prefix) are NOT exempt.
func TestIsCSRFExemptPath_RejectsAttackerControlledPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		path       string
		panelRoot  string
		agentRoot  string
		wantExempt bool
	}{
		// Canonical positive cases — must remain exempt.
		{"plain agent bootstrap empty roots", "/api/agent/bootstrap", "", "", true},
		{"plain agent recover empty roots", "/api/agent/recover-certificate", "", "", true},

		// Attacker-controlled prefixes — must NOT be exempt.
		{"attacker-prefixed bootstrap", "/attacker/api/agent/bootstrap", "", "", false},
		{"path traversal", "/api/agent/bootstrap/../../etc/passwd", "", "", false},
		{"double slash leading", "//api/agent/bootstrap", "", "", false},
		// The bare /api/... form is always exempt regardless of configured roots —
		// agents dial /api/... directly and do not know the panel root path.
		{"bare path still exempt when roots configured", "/api/agent/bootstrap", "/panel", "/agent", true},
		{"panel root match", "/panel/api/agent/bootstrap", "/panel", "/agent", true},
		{"agent root match", "/agent/api/agent/bootstrap", "/panel", "/agent", true},
		{"trailing slash", "/api/agent/bootstrap/", "", "", false},
		{"case folded", "/API/AGENT/BOOTSTRAP", "", "", false},
		{"empty path", "", "", "", false},
		{"only the panel root no suffix", "/panel", "/panel", "", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isCSRFExemptPath(tt.path, tt.panelRoot, tt.agentRoot)
			if got != tt.wantExempt {
				t.Fatalf("isCSRFExemptPath(%q, panel=%q, agent=%q) = %v, want %v",
					tt.path, tt.panelRoot, tt.agentRoot, got, tt.wantExempt)
			}
		})
	}
}
