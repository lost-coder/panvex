package telemt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// IN-M1 (corrected): Telemt answers 202 ACCEPTED when a user was persisted
// to disk but the runtime snapshot has not caught up yet. Telemt activates
// the change ITSELF via an inotify watcher on the config file (~50ms
// debounce) — there is NO HTTP reload endpoint (POST /v1/runtime/reload has
// never existed; requests to it 404). The 202 body carries the connection
// links exactly like 201, so the agent must treat 202 as plain success and
// must never call any /v1/runtime/* route.

func applyTestServer(t *testing.T, applyStatus int) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	runtimeCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v1/runtime/") {
			mu.Lock()
			runtimeCalls++
			mu.Unlock()
			t.Errorf("unexpected request to %s %s: Telemt has no /v1/runtime/* endpoints", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"Route not found"}`))
			return
		}
		// apply: POST /v1/users or PATCH /v1/users/{name}
		w.WriteHeader(applyStatus)
		_, _ = w.Write([]byte(`{"data":{"user":{"links":{"tls":["tg://x"]}}}}`))
	}))
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return runtimeCalls
	}
}

func TestApplyClient202IsSuccessWithoutReload(t *testing.T) {
	srv, runtimeCalls := applyTestServer(t, http.StatusAccepted)
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	res, err := client.CreateClient(context.Background(), ManagedClient{Name: "alice", Secret: "00112233445566778899aabbccddeeff"})
	if err != nil {
		t.Fatalf("CreateClient() error = %v (202 must be plain success)", err)
	}
	if len(res.ConnectionLinks) == 0 {
		t.Fatalf("expected connection links from 202 body, got none")
	}
	if got := runtimeCalls(); got != 0 {
		t.Fatalf("/v1/runtime/* calls = %d, want 0 (Telemt hot-reloads itself)", got)
	}
}

func TestUpdateClient202IsSuccessWithoutReload(t *testing.T) {
	srv, runtimeCalls := applyTestServer(t, http.StatusAccepted)
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	res, err := client.UpdateClient(context.Background(), ManagedClient{Name: "alice", Secret: "00112233445566778899aabbccddeeff"})
	if err != nil {
		t.Fatalf("UpdateClient() error = %v (202 must be plain success)", err)
	}
	if len(res.ConnectionLinks) == 0 {
		t.Fatalf("expected connection links from 202 body, got none")
	}
	if got := runtimeCalls(); got != 0 {
		t.Fatalf("/v1/runtime/* calls = %d, want 0 (Telemt hot-reloads itself)", got)
	}
}

func TestApplyClientNoReloadOn201(t *testing.T) {
	srv, runtimeCalls := applyTestServer(t, http.StatusCreated)
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.CreateClient(context.Background(), ManagedClient{Name: "bob", Secret: "00112233445566778899aabbccddeeff"}); err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}
	if got := runtimeCalls(); got != 0 {
		t.Fatalf("/v1/runtime/* calls = %d, want 0 (201 is already in-runtime)", got)
	}
}
