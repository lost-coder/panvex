package telemt

import (
	"context"
	"encoding/json"
	"io"
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
		case "/v1/users":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"username":            "alice",
					"current_connections": 3,
					"active_unique_ips":   2,
					"total_octets":        1024,
				},
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
	if len(state.Clients) != 1 {
		t.Fatalf("len(state.Clients) = %d, want %d", len(state.Clients), 1)
	}
	if state.Clients[0].ClientName != "alice" {
		t.Fatalf("state.Clients[0].ClientName = %q, want %q", state.Clients[0].ClientName, "alice")
	}
	if state.Clients[0].TrafficUsedBytes != 1024 {
		t.Fatalf("state.Clients[0].TrafficUsedBytes = %d, want %d", state.Clients[0].TrafficUsedBytes, 1024)
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

func TestClientCreateClientCallsTelemtUsersEndpoint(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/users" {
			http.NotFound(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if err := json.Unmarshal(body, &requestBody); err != nil {
			t.Fatalf("json.Unmarshal(request) error = %v", err)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"links": map[string]any{
				"tls": []string{"tg://proxy?server=node-a&secret=tls"},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:       server.URL,
		Authorization: "secret",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	result, err := client.CreateClient(context.Background(), ManagedClient{
		Name:              "alice",
		Secret:            "secret-1",
		UserADTag:         "0123456789abcdef0123456789abcdef",
		Enabled:           true,
		MaxTCPConns:       4,
		MaxUniqueIPs:      2,
		DataQuotaBytes:    1024,
		ExpirationRFC3339: "2026-04-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}

	if requestBody["username"] != "alice" {
		t.Fatalf("request username = %v, want %q", requestBody["username"], "alice")
	}
	if result.ConnectionLink != "tg://proxy?server=node-a&secret=tls" {
		t.Fatalf("result.ConnectionLink = %q, want %q", result.ConnectionLink, "tg://proxy?server=node-a&secret=tls")
	}
}

func TestClientUpdateClientUsesPreviousNameInPath(t *testing.T) {
	var requestPath string
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.NotFound(w, r)
			return
		}

		requestPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if err := json.Unmarshal(body, &requestBody); err != nil {
			t.Fatalf("json.Unmarshal(request) error = %v", err)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"links": map[string]any{
				"secure": []string{"tg://proxy?server=node-a&secret=secure"},
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:       server.URL,
		Authorization: "secret",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	result, err := client.UpdateClient(context.Background(), ManagedClient{
		PreviousName: "alice",
		Name:         "alice-new",
		Secret:       "secret-2",
	})
	if err != nil {
		t.Fatalf("UpdateClient() error = %v", err)
	}

	if requestPath != "/v1/users/alice" {
		t.Fatalf("request path = %q, want %q", requestPath, "/v1/users/alice")
	}
	if requestBody["username"] != "alice-new" {
		t.Fatalf("request username = %v, want %q", requestBody["username"], "alice-new")
	}
	if result.ConnectionLink != "tg://proxy?server=node-a&secret=secure" {
		t.Fatalf("result.ConnectionLink = %q, want %q", result.ConnectionLink, "tg://proxy?server=node-a&secret=secure")
	}
}

func TestClientDeleteClientCallsTelemtUsersEndpoint(t *testing.T) {
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}

		requestPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:       server.URL,
		Authorization: "secret",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if err := client.DeleteClient(context.Background(), "alice"); err != nil {
		t.Fatalf("DeleteClient() error = %v", err)
	}

	if requestPath != "/v1/users/alice" {
		t.Fatalf("request path = %q, want %q", requestPath, "/v1/users/alice")
	}
}
