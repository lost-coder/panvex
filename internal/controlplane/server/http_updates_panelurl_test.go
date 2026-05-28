package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestBuildAgentUpdatePayloadUsesLivePanelURL verifies that the
// panel_proxy_url in an agent-update payload is derived from the live
// OperationalStore (http.public_url) rather than a stale boot-time
// snapshot of s.panelSettings (Plan 3 read-path unification).
func TestBuildAgentUpdatePayloadUsesLivePanelURL(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Store:            store,
	})
	defer server.Close()

	// Seed through the OperationalStore (now the authoritative read path),
	// matching the other Plan 3 tests rather than the legacy
	// store.PutPanelSettings "panel" scope.
	if err := server.settings.Put(context.Background(), map[string]string{
		"http.public_url": "https://panel.example.com",
	}, "test"); err != nil {
		t.Fatalf("settings.Put() error = %v", err)
	}

	assets := agentUpdateAssets{
		targetVersion: "v1.2.3",
		downloadURL:   "https://github.com/example/release/agent",
		signatureURL:  "https://github.com/example/release/agent.sig",
	}
	payloadJSON := server.buildAgentUpdatePayload(
		assets,
		"deadbeef",
		UpdateSettings{AgentDownloadSource: "panel"},
		"internal.example.net",
	)

	var payload struct {
		PanelProxyURL string `json:"panel_proxy_url"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	want := "https://panel.example.com/api/agent/update/binary?version=1.2.3&arch=amd64"
	if payload.PanelProxyURL != want {
		t.Fatalf("panel_proxy_url = %q, want %q", payload.PanelProxyURL, want)
	}
}
