package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// provisionOutboundFixture wires a server bound to a SQLite store, an
// admin session, and a ProvisionOutboundDeps that points at the same
// store. All subtests share the same setup; each builds on top with
// scenario-specific request bodies.
type provisionOutboundFixture struct {
	srv     *Server
	cookies []*http.Cookie
	store   *sqlite.Store
}

func setupProvisionOutboundFixture(t *testing.T, deps *ProvisionOutboundDeps) provisionOutboundFixture {
	t.Helper()
	now := time.Date(2026, time.May, 14, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
		PanelRuntime: PanelRuntime{
			HTTPListenAddress: ":8080",
			GRPCListenAddress: ":8443",
			TLSMode:           "proxy",
		},
	})
	t.Cleanup(func() { srv.Close() })

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	if deps == nil {
		deps = &ProvisionOutboundDeps{
			Queries:    store.Queries(),
			PanelCAPin: "sha256:fakepin",
			PanelCN:    "panel.example.com",
			Now:        func() time.Time { return now },
		}
	}
	srv.SetProvisionOutboundDeps(deps)

	login := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "admin", "password": "Admin1password"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body=%s)", login.Code, login.Body.String())
	}
	return provisionOutboundFixture{
		srv:     srv,
		cookies: login.Result().Cookies(),
		store:   store,
	}
}

// TestProvisionOutboundAgentHappyPath (T-05) covers the success flow:
// admin posts a valid request, the response carries an agent_id +
// pre-baked command + future expiry, and the response's script_url
// reflects the chosen source. The audit log gains a
// `agents.provision_outbound` event keyed on the agent_id (verified
// indirectly via the response, since the audit table is checked in a
// dedicated suite elsewhere).
func TestProvisionOutboundAgentHappyPath(t *testing.T) {
	f := setupProvisionOutboundFixture(t, nil)

	resp := performJSONRequest(t, f.srv, http.MethodPost,
		"/api/agents/provision-outbound",
		map[string]any{
			"node_name":     "edge-fra-01",
			"dial_address":  "203.0.113.10:8443",
			"script_source": "github",
		},
		f.cookies)
	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", resp.Code, resp.Body.String())
	}

	var body provisionOutboundAgentResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, resp.Body.String())
	}
	if body.AgentID == "" {
		t.Fatal("agent_id is empty")
	}
	if body.Command == "" {
		t.Fatal("command is empty")
	}
	if body.ExpiresAtUnix == 0 {
		t.Fatal("expires_at_unix is zero")
	}
	if body.ScriptURL != "https://raw.githubusercontent.com/lost-coder/panvex/main/deploy/install-agent.sh" {
		t.Fatalf("script_url = %q, want github default", body.ScriptURL)
	}
	// Command must carry the agent_id and dial-derived listen bind so
	// the panel and agent agree on identity + transport without an
	// extra round-trip after the agent comes up.
	if !strings.Contains(body.Command, "--agent-id="+body.AgentID) {
		t.Fatalf("command missing --agent-id flag: %s", body.Command)
	}
	if !strings.Contains(body.Command, "--listen-addr=:8443") {
		t.Fatalf("command missing --listen-addr derived from dial port: %s", body.Command)
	}
}

// TestProvisionOutboundAgentRejectsInvalidNodeName (T-06) confirms the
// regex guard catches shell-unsafe input. The wizard pre-validates the
// same way; the server-side check is defence-in-depth so a hand-rolled
// API client cannot inject through this argv slot.
func TestProvisionOutboundAgentRejectsInvalidNodeName(t *testing.T) {
	f := setupProvisionOutboundFixture(t, nil)

	for _, bad := range []string{
		"",                      // empty
		"name with spaces",      // space disallowed
		"node;rm -rf /",         // shell metachar
		strings.Repeat("a", 65), // over 64 chars
		"emoji-😀",               // non-ASCII outside the class
	} {
		resp := performJSONRequest(t, f.srv, http.MethodPost,
			"/api/agents/provision-outbound",
			map[string]any{
				"node_name":    bad,
				"dial_address": "203.0.113.10:8443",
			},
			f.cookies)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("node_name=%q status = %d, want 400 (body=%s)", bad, resp.Code, resp.Body.String())
		}
	}
}

// TestProvisionOutboundAgentRejectsInvalidDialAddress (T-07) covers the
// host:port parse — outbound supervisors need a real network target,
// and the agent's listen-bind is derived from the port.
func TestProvisionOutboundAgentRejectsInvalidDialAddress(t *testing.T) {
	f := setupProvisionOutboundFixture(t, nil)

	for _, bad := range []string{
		"",                                // empty
		"203.0.113.10",                    // no port
		"vps.example.com:",                // empty port
		":8443",                           // empty host (net.SplitHostPort accepts this; our check rejects)
		"vps.example.com:not-a-port:8443", // junk
	} {
		resp := performJSONRequest(t, f.srv, http.MethodPost,
			"/api/agents/provision-outbound",
			map[string]any{
				"node_name":    "edge-01",
				"dial_address": bad,
			},
			f.cookies)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("dial_address=%q status = %d, want 400 (body=%s)", bad, resp.Code, resp.Body.String())
		}
	}
}

// TestProvisionOutboundAgentPanelSourceEmitsSHA256Form (T-11) asserts
// that script_source=panel yields the temp-file + sha256sum branch of
// BuildInstallCommand. The wizard renders this when the agent host can
// reach the panel for bootstrap (rarer for outbound but supported).
func TestProvisionOutboundAgentPanelSourceEmitsSHA256Form(t *testing.T) {
	t.Setenv("PANVEX_INSTALL_SCRIPT_URL", "")
	f := setupProvisionOutboundFixture(t, nil)
	// Panel-source URL is now resolved live from http.public_url; set it so
	// the rendered script_url is deterministic.
	if err := f.srv.settings.Put(context.Background(),
		map[string]string{"http.public_url": "https://panel.example.com"}, "test"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	resp := performJSONRequest(t, f.srv, http.MethodPost,
		"/api/agents/provision-outbound",
		map[string]any{
			"node_name":     "edge-fra-02",
			"dial_address":  "203.0.113.11:8443",
			"script_source": "panel",
		},
		f.cookies)
	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", resp.Code, resp.Body.String())
	}

	var body provisionOutboundAgentResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.ScriptURL != "https://panel.example.com/install-agent.sh" {
		t.Fatalf("script_url = %q, want panel URL", body.ScriptURL)
	}
	// SHA-256 form: mktemp + curl -o + sha256sum + sudo -E bash <file>.
	mustContain := []string{
		"mktemp",
		"sha256sum",
		"sudo -E PANVEX_INSTALL_SCRIPT_SHA256=",
	}
	for _, want := range mustContain {
		if !strings.Contains(body.Command, want) {
			t.Fatalf("panel-source command missing %q\n%s", want, body.Command)
		}
	}
}

// TestProvisionOutboundAgentGitHubSourceEmitsLegacyForm (T-12) asserts
// the github source yields the plain curl|sudo-bash form (no hash
// verification — the panel cannot vouch for upstream bytes).
func TestProvisionOutboundAgentGitHubSourceEmitsLegacyForm(t *testing.T) {
	f := setupProvisionOutboundFixture(t, nil)

	resp := performJSONRequest(t, f.srv, http.MethodPost,
		"/api/agents/provision-outbound",
		map[string]any{
			"node_name":     "edge-fra-03",
			"dial_address":  "203.0.113.12:8443",
			"script_source": "github",
		},
		f.cookies)
	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", resp.Code, resp.Body.String())
	}

	var body provisionOutboundAgentResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Legacy form: `curl ... | sudo bash -s -- ...`. mktemp / sha256sum
	// MUST be absent so a fork of install-agent.sh on a private mirror
	// (which the panel hasn't hashed) does not hard-fail on a
	// digest mismatch.
	if !strings.Contains(body.Command, "| sudo bash -s --") {
		t.Fatalf("github-source command missing legacy curl|bash form:\n%s", body.Command)
	}
	for _, forbidden := range []string{"mktemp", "sha256sum", "PANVEX_INSTALL_SCRIPT_SHA256"} {
		if strings.Contains(body.Command, forbidden) {
			t.Fatalf("github-source command contains %q (panel cannot vouch for upstream bytes):\n%s", forbidden, body.Command)
		}
	}
}

// TestProvisionOutboundUsesLivePanelScriptURL pins Plan 4: the panel-source
// install URL must be derived from the LIVE http.public_url setting per
// request, so editing it in the panel changes the rendered command without a
// restart (instead of a value frozen at process start).
func TestProvisionOutboundUsesLivePanelScriptURL(t *testing.T) {
	t.Setenv("PANVEX_INSTALL_SCRIPT_URL", "") // ensure no override masks the live URL
	f := setupProvisionOutboundFixture(t, nil)
	if err := f.srv.settings.Put(context.Background(),
		map[string]string{"http.public_url": "https://live-panel.example"}, "test"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	resp := performJSONRequest(t, f.srv, http.MethodPost,
		"/api/agents/provision-outbound",
		map[string]any{
			"node_name":     "edge-fra-99",
			"dial_address":  "203.0.113.20:8443",
			"script_source": "panel",
		},
		f.cookies)
	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", resp.Code, resp.Body.String())
	}

	var body provisionOutboundAgentResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	const wantURL = "https://live-panel.example/install-agent.sh"
	if body.ScriptURL != wantURL {
		t.Fatalf("script_url = %q, want live %q", body.ScriptURL, wantURL)
	}
	if !strings.Contains(body.Command, wantURL) {
		t.Fatalf("command missing live script URL %q:\n%s", wantURL, body.Command)
	}
}

// TestProvisionOutboundUsesLivePanelGRPCEndpoint pins Plan 4: the
// --panel-url-grpc flag baked into the install command must be derived from the
// LIVE grpc.public_endpoint setting per request, so editing it in the panel
// changes the rendered command without a restart (mirrors the http.public_url
// live-resolution test above, for the gRPC endpoint).
func TestProvisionOutboundUsesLivePanelGRPCEndpoint(t *testing.T) {
	t.Setenv("PANVEX_INSTALL_SCRIPT_URL", "") // ensure no override masks the live URL
	f := setupProvisionOutboundFixture(t, nil)
	if err := f.srv.settings.Put(context.Background(),
		map[string]string{"grpc.public_endpoint": "grpc-live.example:443"}, "test"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	resp := performJSONRequest(t, f.srv, http.MethodPost,
		"/api/agents/provision-outbound",
		map[string]any{
			"node_name":     "edge-fra-98",
			"dial_address":  "203.0.113.21:8443",
			"script_source": "panel",
		},
		f.cookies)
	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", resp.Code, resp.Body.String())
	}

	var body provisionOutboundAgentResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(body.Command, "--panel-url-grpc=grpc-live.example:443") {
		t.Fatalf("command missing live panel gRPC endpoint:\n%s", body.Command)
	}
}

// TestProvisionOutboundAgentReturns503WhenUnconfigured guards the
// nil-deps path: if cmd/control-plane forgets to call
// SetProvisionOutboundDeps (e.g. storage backend without Queries()),
// the route returns 503 instead of crashing on a nil dereference.
func TestProvisionOutboundAgentReturns503WhenUnconfigured(t *testing.T) {
	// Pass an explicit deps==nil so the fixture skips its default
	// wiring. Then null it again post-setup to simulate the
	// "Queries-less store" path.
	f := setupProvisionOutboundFixture(t, &ProvisionOutboundDeps{Queries: nil})
	f.srv.SetProvisionOutboundDeps(nil)

	resp := performJSONRequest(t, f.srv, http.MethodPost,
		"/api/agents/provision-outbound",
		map[string]any{
			"node_name":    "edge-01",
			"dial_address": "203.0.113.10:8443",
		},
		f.cookies)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body=%s)", resp.Code, resp.Body.String())
	}
}

// TestProvisionOutboundAgentRequiresAdminRole verifies the role guard
// rejects operators and viewers — the endpoint creates DB rows and
// mints bootstrap tokens, neither of which is appropriate for non-
// admin roles.
func TestProvisionOutboundAgentRequiresAdminRole(t *testing.T) {
	now := time.Date(2026, time.May, 14, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
		PanelRuntime: PanelRuntime{
			HTTPListenAddress: ":8080",
			GRPCListenAddress: ":8443",
			TLSMode:           "proxy",
		},
	})
	t.Cleanup(func() { srv.Close() })

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "operator",
		Password: "Operator1password",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	srv.SetProvisionOutboundDeps(&ProvisionOutboundDeps{
		Queries: store.Queries(),
		Now:     func() time.Time { return now },
	})

	login := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "operator", "password": "Operator1password"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d", login.Code)
	}

	resp := performJSONRequest(t, srv, http.MethodPost,
		"/api/agents/provision-outbound",
		map[string]any{
			"node_name":    "edge-01",
			"dial_address": "203.0.113.10:8443",
		},
		login.Result().Cookies())
	if resp.Code != http.StatusForbidden {
		t.Fatalf("operator status = %d, want 403 (body=%s)", resp.Code, resp.Body.String())
	}
}
