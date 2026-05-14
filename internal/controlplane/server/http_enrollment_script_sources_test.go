package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// scriptSourcesPayload mirrors the JSON shape emitted by
// createEnrollmentTokenResponse — duplicated here (instead of importing
// the unexported types) so the test asserts the wire format byte-for-
// byte rather than the Go struct's `MarshalJSON` happy path.
type scriptSourcesPayload struct {
	Panel  scriptSourcePayload `json:"panel"`
	GitHub scriptSourcePayload `json:"github"`
}

type scriptSourcePayload struct {
	URL    string  `json:"url"`
	SHA256 *string `json:"sha256"`
}

type enrollmentResponseWithSources struct {
	ScriptSources scriptSourcesPayload `json:"script_sources"`
	PanelURL      string               `json:"panel_url"`
}

// TestCreateEnrollmentTokenIncludesScriptSources asserts the PR-2a
// contract: every successful POST /api/agents/enrollment-tokens
// includes both Panel and GitHub install-script source pointers, the
// panel source carries the embedded body's SHA-256, and the GitHub
// source carries `null` for sha256 (panel cannot vouch for bytes
// hosted at a URL it does not control). See [docs/specs/2026-05-14-
// add-server-wizard-outbound-mode.md] §4.5.
func TestCreateEnrollmentTokenIncludesScriptSources(t *testing.T) {
	now := time.Date(2026, time.May, 14, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

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
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "operator",
		Password: "Operator1password",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	login := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "operator", "password": "Operator1password"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body=%s)", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()

	create := performJSONRequestWithHeaders(
		t, srv, http.MethodPost,
		"https://panel.example.com/api/agents/enrollment-tokens",
		map[string]any{"fleet_group_id": "default", "ttl_seconds": 600},
		cookies, nil,
	)
	if create.Code != http.StatusCreated {
		t.Fatalf("create-token status = %d, want %d (body=%s)", create.Code, http.StatusCreated, create.Body.String())
	}

	var body enrollmentResponseWithSources
	if err := json.Unmarshal(create.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, create.Body.String())
	}

	// Panel source: URL derives from the panel public URL the request
	// hit (panel.example.com), with the conventional /install-agent.sh
	// suffix. The sha256 must be the embedded script's digest — that
	// is, the exact bytes a GET /install-agent.sh would return.
	wantPanelURL := body.PanelURL + "/install-agent.sh"
	if body.ScriptSources.Panel.URL != wantPanelURL {
		t.Fatalf("panel.url = %q, want %q", body.ScriptSources.Panel.URL, wantPanelURL)
	}
	if body.ScriptSources.Panel.SHA256 == nil {
		t.Fatal("panel.sha256 is null, want 64-hex digest")
	}
	if got := *body.ScriptSources.Panel.SHA256; len(got) != 64 {
		t.Fatalf("panel.sha256 length = %d, want 64 hex chars (%q)", len(got), got)
	}
	if got := *body.ScriptSources.Panel.SHA256; got != InstallScriptSHA256() {
		t.Fatalf("panel.sha256 = %q, want embedded-script digest %q", got, InstallScriptSHA256())
	}

	// GitHub source: default URL + null sha256. The default is the
	// upstream raw URL — operators forking the project override via
	// PANVEX_INSTALL_SCRIPT_GITHUB_URL (covered separately).
	if body.ScriptSources.GitHub.URL != defaultInstallScriptGitHubURL {
		t.Fatalf("github.url = %q, want %q", body.ScriptSources.GitHub.URL, defaultInstallScriptGitHubURL)
	}
	if body.ScriptSources.GitHub.SHA256 != nil {
		t.Fatalf("github.sha256 = %v, want null", *body.ScriptSources.GitHub.SHA256)
	}
}

// TestInstallScriptGitHubURLHonoursEnvOverride asserts the
// PANVEX_INSTALL_SCRIPT_GITHUB_URL env var redirects the GitHub
// install-script pointer at a fork / private mirror. The wizard reads
// the URL the panel emits, so misconfiguration here would silently
// downgrade operators to upstream when they expect their own mirror.
func TestInstallScriptGitHubURLHonoursEnvOverride(t *testing.T) {
	const override = "https://raw.githubusercontent.com/example/panvex-fork/v1.2.3/deploy/install-agent.sh"
	t.Setenv("PANVEX_INSTALL_SCRIPT_GITHUB_URL", override)

	got := InstallScriptGitHubURL()
	if got != override {
		t.Fatalf("InstallScriptGitHubURL() = %q, want %q", got, override)
	}
}

// TestInstallScriptPanelURLHonoursEnvOverride asserts the existing
// PANVEX_INSTALL_SCRIPT_URL env var still wins over the derived panel
// URL — the legacy override path (Q-05) must keep working after the
// PR-2a additions.
func TestInstallScriptPanelURLHonoursEnvOverride(t *testing.T) {
	const override = "https://cdn.example.com/panvex/install-agent.sh"
	t.Setenv("PANVEX_INSTALL_SCRIPT_URL", override)

	got := installScriptPanelURL("https://panel.example.com")
	if got != override {
		t.Fatalf("installScriptPanelURL() = %q, want %q", got, override)
	}
}

// TestInstallScriptPanelURLDerivesFromPanelURL asserts that without
// an env override, the panel source URL is derived from the panel
// public URL the wizard sees — same authoritative value as the outer
// `panel_url` field, so the two cannot drift.
func TestInstallScriptPanelURLDerivesFromPanelURL(t *testing.T) {
	t.Setenv("PANVEX_INSTALL_SCRIPT_URL", "")
	got := installScriptPanelURL("https://panel.example.com/panvex")
	const want = "https://panel.example.com/panvex/install-agent.sh"
	if got != want {
		t.Fatalf("installScriptPanelURL() = %q, want %q", got, want)
	}
	// Trailing slash on the panel URL must not produce a double slash.
	got = installScriptPanelURL("https://panel.example.com/panvex/")
	if got != want {
		t.Fatalf("installScriptPanelURL(trailing-slash) = %q, want %q", got, want)
	}
}
