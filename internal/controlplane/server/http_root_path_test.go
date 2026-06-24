package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
)

func TestHTTPRootPathPrefixesAPIRoutesAndEmbeddedUI(t *testing.T) {
	now := time.Date(2026, time.March, 16, 22, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now: func() time.Time { return now },
		UIFiles: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte(`<!doctype html><html><head><script type="module" src="/assets/app.js"></script><link rel="stylesheet" href="/assets/app.css"></head><body><div id="root"></div></body></html>`)},
			"assets/app.js": &fstest.MapFile{Data: []byte("console.log('panvex')")},
			"assets/app.css": &fstest.MapFile{Data: []byte("body { color: black; }")},
		},
		PanelRuntime: PanelRuntime{
			HTTPRootPath: "/panvex",
		},
	})
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "viewer",
		Password: "Viewer1password",
		Role:     auth.RoleViewer,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	prefixedLogin := performJSONRequest(t, server, http.MethodPost, "/panvex/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "Viewer1password",
	}, nil)
	if prefixedLogin.Code != http.StatusOK {
		t.Fatalf("POST /panvex/api/auth/login status = %d, want %d", prefixedLogin.Code, http.StatusOK)
	}

	unprefixedLogin := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "Viewer1password",
	}, nil)
	if unprefixedLogin.Code != http.StatusNotFound {
		t.Fatalf("POST /api/auth/login status = %d, want %d", unprefixedLogin.Code, http.StatusNotFound)
	}

	uiIndex := performRequest(t, server.Handler(), http.MethodGet, "/panvex", nil)
	if uiIndex.Code != http.StatusOK {
		t.Fatalf("GET /panvex status = %d, want %d", uiIndex.Code, http.StatusOK)
	}
	body := uiIndex.Body.String()
	if !strings.Contains(body, `/panvex/assets/app.js`) {
		t.Fatalf("GET /panvex body missing prefixed js asset: %q", body)
	}
	if !strings.Contains(body, `/panvex/assets/app.css`) {
		t.Fatalf("GET /panvex body missing prefixed css asset: %q", body)
	}
	if !strings.Contains(body, `data-root-path="/panvex"`) {
		t.Fatalf("GET /panvex body missing injected root path marker: %q", body)
	}
	if strings.Contains(body, `window.__PANVEX_ROOT_PATH`) {
		t.Fatalf("GET /panvex body contains legacy inline root-path script (must be data-attribute): %q", body)
	}

	uiRoute := performRequest(t, server.Handler(), http.MethodGet, "/panvex/fleet/agent-1", nil)
	if uiRoute.Code != http.StatusOK {
		t.Fatalf("GET /panvex/fleet/agent-1 status = %d, want %d", uiRoute.Code, http.StatusOK)
	}

	prefixedAsset := performRequest(t, server.Handler(), http.MethodGet, "/panvex/assets/app.js", nil)
	if prefixedAsset.Code != http.StatusOK {
		t.Fatalf("GET /panvex/assets/app.js status = %d, want %d", prefixedAsset.Code, http.StatusOK)
	}

	unprefixedAsset := performRequest(t, server.Handler(), http.MethodGet, "/assets/app.js", nil)
	if unprefixedAsset.Code != http.StatusNotFound {
		t.Fatalf("GET /assets/app.js status = %d, want %d", unprefixedAsset.Code, http.StatusNotFound)
	}
}

// TestInstallAgentScriptReachableUnderAgentRootPath guards the contract
// that the install-script URL advertised in createEnrollmentTokenResponse
// (built via installScriptPanelURL(buildAgentPublicURL(...))) actually
// resolves to a 200 when agent_root_path is distinct from root_path.
// Without the outer-mux mirror in routes(), curl <agent_root>/install-
// agent.sh returns 404 because the inner /install-agent.sh route is only
// reachable through stripRootPath(panelPath, ...).
func TestInstallAgentScriptReachableUnderAgentRootPath(t *testing.T) {
	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		PanelRuntime: PanelRuntime{
			HTTPRootPath:      "/m_APxHhG",
			AgentHTTPRootPath: "/bi7jXFcP",
			TLSMode:           "proxy",
		},
	})
	defer server.Close()

	handler := server.Handler()

	panelHit := performRequest(t, handler, http.MethodGet, "/m_APxHhG/install-agent.sh", nil)
	if panelHit.Code != http.StatusOK {
		t.Fatalf("GET /m_APxHhG/install-agent.sh status = %d, want %d", panelHit.Code, http.StatusOK)
	}
	if panelHit.Header().Get("X-Install-Script-SHA256") == "" {
		t.Fatalf("GET /m_APxHhG/install-agent.sh missing X-Install-Script-SHA256 header")
	}

	agentHit := performRequest(t, handler, http.MethodGet, "/bi7jXFcP/install-agent.sh", nil)
	if agentHit.Code != http.StatusOK {
		t.Fatalf("GET /bi7jXFcP/install-agent.sh status = %d, want %d (the URL the enrollment-token panel.url points at)", agentHit.Code, http.StatusOK)
	}
	if agentHit.Header().Get("X-Install-Script-SHA256") == "" {
		t.Fatalf("GET /bi7jXFcP/install-agent.sh missing X-Install-Script-SHA256 header")
	}
	if panelHit.Body.String() != agentHit.Body.String() {
		t.Fatalf("install-agent.sh body differs between panel and agent paths")
	}
}
