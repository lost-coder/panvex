package telemt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClientRejectsNonLoopbackEndpoint(t *testing.T) {
	_, err := NewClient(Config{
		BaseURL:       "http://10.0.0.10:8080",
		Authorization: "secret",
	}, http.DefaultClient)
	if err == nil {
		t.Fatal("NewClient() error = nil, want loopback validation failure")
	}

	if err != ErrNonLoopbackEndpoint {
		t.Fatalf("NewClient() error = %v, want %v", err, ErrNonLoopbackEndpoint)
	}
}

func TestNewClientAcceptsLoopbackEndpoint(t *testing.T) {
	client, err := NewClient(Config{
		BaseURL:       "http://127.0.0.1:8080",
		Authorization: "secret",
	}, http.DefaultClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if client.baseURL.Host != "127.0.0.1:8080" {
		t.Fatalf("client.baseURL.Host = %q, want %q", client.baseURL.Host, "127.0.0.1:8080")
	}
}

func TestClientFetchRuntimeStateUsesLoopbackAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version": "2026.03",
			})
		case "/v1/security/posture":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"read_only": true,
			})
		case "/v1/stats/summary":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"active_connections": 42,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:       server.URL,
		Authorization: "secret",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	state, err := client.FetchRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("FetchRuntimeState() error = %v", err)
	}

	if !state.ReadOnly {
		t.Fatal("state.ReadOnly = false, want true")
	}

	if state.ConnectedUsers != 42 {
		t.Fatalf("state.ConnectedUsers = %d, want %d", state.ConnectedUsers, 42)
	}
}

func TestClientExecuteRuntimeReloadCallsTelemtEndpoint(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runtime/reload" {
			http.NotFound(w, r)
			return
		}

		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:       server.URL,
		Authorization: "secret",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if err := client.ExecuteRuntimeReload(context.Background()); err != nil {
		t.Fatalf("ExecuteRuntimeReload() error = %v", err)
	}

	if !called {
		t.Fatal("ExecuteRuntimeReload() did not call Telemt runtime reload endpoint")
	}
}
