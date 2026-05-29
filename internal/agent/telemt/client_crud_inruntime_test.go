package telemt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// IN-M1: Telemt answers 202 ACCEPTED when a user was written to disk but is
// not yet in the live runtime (in_runtime=false) — a reload is required
// before the client is actually serving. Previously the agent treated any
// <400 as fully applied, so the panel reported "succeeded" with links while
// the node had not activated the client. The agent must now auto-reload on
// 202 so success reflects an in-runtime client.

func applyTestServer(t *testing.T, applyStatus int) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	reloadCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/runtime/reload":
			mu.Lock()
			reloadCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{}}`))
		default: // apply: POST /v1/users
			w.WriteHeader(applyStatus)
			_, _ = w.Write([]byte(`{"data":{"user":{"links":{"tls":["tg://x"]}}}}`))
		}
	}))
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return reloadCalls
	}
}

func TestApplyClientAutoReloadsOn202(t *testing.T) {
	srv, reloadCalls := applyTestServer(t, http.StatusAccepted)
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	res, err := client.CreateClient(context.Background(), ManagedClient{Name: "alice", Secret: "00112233445566778899aabbccddeeff"})
	if err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}
	if len(res.ConnectionLinks) == 0 {
		t.Fatalf("expected connection links from 202 body, got none")
	}
	if got := reloadCalls(); got != 1 {
		t.Fatalf("runtime reload calls = %d, want 1 (202 must trigger auto-reload)", got)
	}
}

func TestApplyClientNoReloadOn201(t *testing.T) {
	srv, reloadCalls := applyTestServer(t, http.StatusCreated)
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.CreateClient(context.Background(), ManagedClient{Name: "bob", Secret: "00112233445566778899aabbccddeeff"}); err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}
	if got := reloadCalls(); got != 0 {
		t.Fatalf("runtime reload calls = %d, want 0 (201 in_runtime needs no reload)", got)
	}
}
