package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/security"
)

func TestHTTPAgentBootstrapUsesConfiguredGRPCPublicEndpoint(t *testing.T) {
	now := time.Date(2026, time.March, 16, 20, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()

	// The OperationalStore is now the authoritative read path for panel
	// settings (Plan 3), so seed through it rather than the legacy
	// store.PutPanelSettings (which writes the separate "panel" scope).
	if err := server.settings.Put(context.Background(), map[string]string{
		"http.public_url":      "https://panel.example.com",
		"grpc.public_endpoint": "grpc.panel.example.com:443",
	}, "test"); err != nil {
		t.Fatalf("settings.Put() error = %v", err)
	}
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "default",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	bootstrapResponse := performJSONRequestWithHeaders(
		t,
		server,
		http.MethodPost,
		"https://internal.example.net/api/agent/bootstrap",
		map[string]string{
			"node_name": "node-a",
			"version":   "1.0.0",
		},
		nil,
		map[string]string{
			"Authorization": "Bearer " + token.Value,
		},
	)
	if bootstrapResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/agent/bootstrap status = %d, want %d", bootstrapResponse.Code, http.StatusOK)
	}

	var payload struct {
		GRPCEndpoint string `json:"grpc_endpoint"`
	}
	if err := json.Unmarshal(bootstrapResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.GRPCEndpoint != "grpc.panel.example.com:443" {
		t.Fatalf("bootstrap.grpc_endpoint = %q, want %q", payload.GRPCEndpoint, "grpc.panel.example.com:443")
	}
}
