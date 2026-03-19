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
			writeSuccessEnvelope(w, map[string]any{
				"status": "ok",
			})
		case "/v1/security/posture":
			writeSuccessEnvelope(w, map[string]any{
				"read_only": true,
			})
		case "/v1/system/info":
			writeSuccessEnvelope(w, map[string]any{
				"version": "2026.03",
			})
		case "/v1/runtime/gates":
			writeSuccessEnvelope(w, map[string]any{
				"accepting_new_connections": true,
				"me_runtime_ready":          true,
				"me2dc_fallback_enabled":   true,
				"use_middle_proxy":         true,
				"startup_status":           "ready",
				"startup_stage":            "serving",
				"startup_progress_pct":     100.0,
			})
		case "/v1/runtime/initialization":
			writeSuccessEnvelope(w, map[string]any{
				"status":         "ready",
				"degraded":       false,
				"current_stage":  "serving",
				"progress_pct":   100.0,
				"transport_mode": "middle_proxy",
			})
		case "/v1/runtime/connections/summary":
			writeSuccessEnvelope(w, map[string]any{
				"enabled": true,
				"data": map[string]any{
					"totals": map[string]any{
						"current_connections":        42,
						"current_connections_me":     39,
						"current_connections_direct": 3,
						"active_users":               7,
					},
				},
			})
		case "/v1/stats/summary":
			writeSuccessEnvelope(w, map[string]any{
				"connections_total":         512,
				"connections_bad_total":     9,
				"handshake_timeouts_total":  4,
				"configured_users":          12,
			})
		case "/v1/stats/dcs":
			writeSuccessEnvelope(w, map[string]any{
				"dcs": []map[string]any{
					{
						"dc":                  2,
						"available_endpoints": 3,
						"available_pct":       100.0,
						"required_writers":    4,
						"alive_writers":       4,
						"coverage_pct":        100.0,
						"rtt_ms":              21.5,
						"load":                18,
					},
				},
			})
		case "/v1/stats/upstreams":
			writeSuccessEnvelope(w, map[string]any{
				"summary": map[string]any{
					"configured_total": 2,
					"healthy_total":    1,
					"unhealthy_total":  1,
					"direct_total":     1,
					"socks5_total":     1,
				},
				"upstreams": []map[string]any{
					{
						"upstream_id":           1,
						"route_kind":            "direct",
						"address":               "direct",
						"healthy":               true,
						"fails":                 0,
						"effective_latency_ms":  11.2,
					},
				},
			})
		case "/v1/runtime/events/recent":
			writeSuccessEnvelope(w, map[string]any{
				"enabled": true,
				"data": map[string]any{
					"events": []map[string]any{
						{
							"seq":           1,
							"ts_epoch_secs": 1_763_226_400,
							"event_type":    "upstream_recovered",
							"context":       "dc=2 upstream=1",
						},
					},
				},
			})
		case "/v1/stats/users":
			writeSuccessEnvelope(w, []map[string]any{
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
	if !state.Gates.AcceptingNewConnections {
		t.Fatal("state.Gates.AcceptingNewConnections = false, want true")
	}
	if state.Initialization.TransportMode != "middle_proxy" {
		t.Fatalf("state.Initialization.TransportMode = %q, want %q", state.Initialization.TransportMode, "middle_proxy")
	}
	if state.ConnectionTotals.CurrentConnectionsME != 39 {
		t.Fatalf("state.ConnectionTotals.CurrentConnectionsME = %d, want %d", state.ConnectionTotals.CurrentConnectionsME, 39)
	}
	if state.Summary.ConnectionsTotal != 512 {
		t.Fatalf("state.Summary.ConnectionsTotal = %d, want %d", state.Summary.ConnectionsTotal, 512)
	}
	if len(state.DCs) != 1 {
		t.Fatalf("len(state.DCs) = %d, want %d", len(state.DCs), 1)
	}
	if state.DCs[0].CoveragePct != 100 {
		t.Fatalf("state.DCs[0].CoveragePct = %v, want %v", state.DCs[0].CoveragePct, 100.0)
	}
	if state.Upstreams.HealthyTotal != 1 {
		t.Fatalf("state.Upstreams.HealthyTotal = %d, want %d", state.Upstreams.HealthyTotal, 1)
	}
	if len(state.RecentEvents) != 1 {
		t.Fatalf("len(state.RecentEvents) = %d, want %d", len(state.RecentEvents), 1)
	}
	if state.RecentEvents[0].EventType != "upstream_recovered" {
		t.Fatalf("state.RecentEvents[0].EventType = %q, want %q", state.RecentEvents[0].EventType, "upstream_recovered")
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

func TestClientCreateClientOmitsEmptyExpiration(t *testing.T) {
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

	if _, err := client.CreateClient(context.Background(), ManagedClient{
		Name:      "alice",
		Secret:    "secret-1",
		UserADTag: "0123456789abcdef0123456789abcdef",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}

	if _, ok := requestBody["expiration_rfc3339"]; ok {
		t.Fatal("requestBody contains expiration_rfc3339, want omitted empty expiration")
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

func TestClientCreateClientReturnsDetailedTelemtError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/users" {
			http.NotFound(w, r)
			return
		}

		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      false,
			"error":   "bad_request",
			"message": "secret must contain exactly 32 hexadecimal characters",
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

	_, err = client.CreateClient(context.Background(), ManagedClient{
		Name:      "alice",
		Secret:    "secret-1",
		UserADTag: "0123456789abcdef0123456789abcdef",
		Enabled:   true,
	})
	if err == nil {
		t.Fatal("CreateClient() error = nil, want detailed Telemt error")
	}
	if err.Error() == "apply client failed with status 400" {
		t.Fatalf("CreateClient() error = %q, want detailed non-generic error", err.Error())
	}
}

func writeSuccessEnvelope(w http.ResponseWriter, data any) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"data":     data,
		"revision": "test-revision",
	})
}
