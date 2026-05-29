package server

import (
	"encoding/json"
	"testing"
)

// TestBuildAgentDirectUpdatePayload verifies the agent-update payload carries
// a release_base_url derived from the configured repo + target version, and
// no longer embeds arch-specific URLs (the agent resolves those itself).
func TestBuildAgentDirectUpdatePayload(t *testing.T) {
	payloadJSON, err := buildAgentDirectUpdatePayload("lost-coder/panvex", "1.2.3")
	if err != nil {
		t.Fatalf("buildAgentDirectUpdatePayload() error = %v", err)
	}

	var payload struct {
		Version        string `json:"version"`
		ReleaseBaseURL string `json:"release_base_url"`
		DownloadURL    string `json:"download_url"`
		PanelProxyURL  string `json:"panel_proxy_url"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	const wantBase = "https://github.com/lost-coder/panvex/releases/download/agent/v1.2.3"
	if payload.ReleaseBaseURL != wantBase {
		t.Fatalf("release_base_url = %q, want %q", payload.ReleaseBaseURL, wantBase)
	}
	if payload.Version != "1.2.3" {
		t.Fatalf("version = %q, want %q", payload.Version, "1.2.3")
	}
	if payload.DownloadURL != "" || payload.PanelProxyURL != "" {
		t.Fatalf("legacy URL fields must be gone, got download_url=%q panel_proxy_url=%q",
			payload.DownloadURL, payload.PanelProxyURL)
	}

	// A v-prefixed version must normalise to the bare form in both fields.
	vPayloadJSON, err := buildAgentDirectUpdatePayload("lost-coder/panvex", "v2.0.0")
	if err != nil {
		t.Fatalf("buildAgentDirectUpdatePayload(v-prefixed) error = %v", err)
	}
	var vp struct {
		Version        string `json:"version"`
		ReleaseBaseURL string `json:"release_base_url"`
	}
	if err := json.Unmarshal(vPayloadJSON, &vp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if vp.Version != "2.0.0" {
		t.Fatalf("v-prefixed version not normalised: got %q, want %q", vp.Version, "2.0.0")
	}
	if vp.ReleaseBaseURL != "https://github.com/lost-coder/panvex/releases/download/agent/v2.0.0" {
		t.Fatalf("release_base_url = %q, want .../agent/v2.0.0", vp.ReleaseBaseURL)
	}
}
