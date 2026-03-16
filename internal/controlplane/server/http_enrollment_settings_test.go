package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
	"github.com/panvex/panvex/internal/security"
)

func TestHTTPAgentBootstrapUsesConfiguredGRPCPublicEndpoint(t *testing.T) {
	now := time.Date(2026, time.March, 16, 20, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if err := store.PutPanelSettings(context.Background(), storage.PanelSettingsRecord{
		HTTPPublicURL:      "https://panel.example.com",
		HTTPRootPath:       "",
		GRPCPublicEndpoint: "grpc.panel.example.com:443",
		HTTPListenAddress:  ":8080",
		GRPCListenAddress:  ":8443",
		TLSMode:            "proxy",
		TLSCertFile:        "",
		TLSKeyFile:         "",
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("PutPanelSettings() error = %v", err)
	}

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		EnvironmentID: "prod",
		FleetGroupID:  "default",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	bootstrapResponse := performJSONRequestWithHeaders(
		t,
		server.Handler(),
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
