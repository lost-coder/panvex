package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestResolveInstallScriptURLUsesLivePublicURL pins Plan 4: the install
// script URL must be derived from the LIVE http.public_url setting per
// request, so editing it in the panel changes the install command without
// a restart.
func TestResolveInstallScriptURLUsesLivePublicURL(t *testing.T) {
	t.Setenv("PANVEX_INSTALL_SCRIPT_URL", "") // ensure no override masks the live URL
	srv := testServerWithSQLite(t, time.Now())
	ctx := context.Background()
	if err := srv.settings.Put(ctx, map[string]string{"http.public_url": "https://panel.example"}, "test"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/agents/a1/install-command", nil)
	got := srv.ResolveInstallScriptURL(req)
	if got != "https://panel.example/install-agent.sh" {
		t.Fatalf("ResolveInstallScriptURL = %q, want %q", got, "https://panel.example/install-agent.sh")
	}
}

// TestResolveAgentGRPCEndpointUsesLiveEndpoint pins Plan 4: the gRPC
// endpoint must be derived from the LIVE grpc.public_endpoint setting per
// request.
func TestResolveAgentGRPCEndpointUsesLiveEndpoint(t *testing.T) {
	srv := testServerWithSQLite(t, time.Now())
	ctx := context.Background()
	if err := srv.settings.Put(ctx, map[string]string{"grpc.public_endpoint": "grpc.example:8443"}, "test"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/agents/a1/install-command", nil)
	if got := srv.ResolveAgentGRPCEndpoint(req); got != "grpc.example:8443" {
		t.Fatalf("ResolveAgentGRPCEndpoint = %q, want %q", got, "grpc.example:8443")
	}
}
