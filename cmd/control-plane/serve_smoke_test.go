package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/server"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestServeBindsHTTPListenerFromStore is Plan 6's hard-gate startup smoke
// test. It proves the riskiest part of the change: the HTTP listener binds
// from the live settings store (seeded from the PANVEX_HTTP_ADDR env override
// inside server.New) rather than from frozen PanelRuntime fields, and that the
// real panel handler actually serves on that store-resolved address.
//
// It exercises the same bind sequence runServe uses:
//
//	api := newAPIServer(...)                       // seeds + reloads the store
//	addr := api.EffectiveHTTPListenAddress()       // store-resolved address
//	listener, _ := net.Listen("tcp", addr)         // bindable?
//	httpServer.Serve(listener)                     // serves the real handler
//
// We bind the listener explicitly (instead of newControlPlaneHTTPServer's
// ListenAndServe) only so the test can recover the ephemeral port chosen for
// 127.0.0.1:0; the address fed to net.Listen is still the store-resolved one.
func TestServeBindsHTTPListenerFromStore(t *testing.T) {
	// Ephemeral port via the env override path — proves PANVEX_HTTP_ADDR still
	// hard-overrides at startup and flows through the store into the bind.
	t.Setenv("PANVEX_HTTP_ADDR", "127.0.0.1:0")

	dbPath := filepath.Join(t.TempDir(), "panvex.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	restartRequests := make(chan struct{}, 1)
	api, err := newAPIServer(
		serveConfig{},
		store,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		// PanelRuntime intentionally carries no listen address: the address
		// must come from the store, not these (now-fallback) fields.
		server.PanelRuntime{TLSMode: "proxy"},
		restartRequests,
	)
	if err != nil {
		t.Fatalf("newAPIServer() error = %v", err)
	}
	t.Cleanup(api.Close)

	addr := api.EffectiveHTTPListenAddress()
	if addr != "127.0.0.1:0" {
		t.Fatalf("EffectiveHTTPListenAddress() = %q, want %q (env override through the store)", addr, "127.0.0.1:0")
	}

	// Bind from the store-resolved address — the core safety assertion.
	// Mirror serve.go's ctx-aware ListenConfig pattern (noctx enforced).
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("ListenConfig.Listen(%q) error = %v", addr, err)
	}

	httpServer := newControlPlaneHTTPServer(addr, api.Handler())
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	})

	// Confirm the panel actually serves on the bound (store-resolved) address:
	// /healthz is an unauthenticated route.
	url := "http://" + listener.Addr().String() + "/healthz"
	var resp *http.Response
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err = http.Get(url) //nolint:gosec,noctx // G107: test requests a controlled loopback URL; short-lived smoke probe
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s never succeeded: %v", url, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz on store-bound listener: status = %d, want 200", resp.StatusCode)
	}

	// Sanity: the listener really used an ephemeral port (not literally :0).
	if _, port, _ := net.SplitHostPort(listener.Addr().String()); port == "0" || port == "" {
		t.Fatalf("listener bound to a bogus port %q", port)
	}
}
