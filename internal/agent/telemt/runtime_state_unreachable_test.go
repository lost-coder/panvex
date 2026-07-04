package telemt

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newRuntimeStateTestClient wires a telemt Client against the given
// httptest server (loopback host satisfies NewClient's guard).
func newRuntimeStateTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(Config{BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestFetchRuntimeStateAllCoreDownReturnsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "telemt down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newRuntimeStateTestClient(t, srv)
	_, err := client.FetchRuntimeState(context.Background())
	if !errors.Is(err, ErrTelemtCoreUnreachable) {
		t.Fatalf("err = %v, want errors.Is(_, ErrTelemtCoreUnreachable)", err)
	}
}

func TestFetchRuntimeStateSingleCoreFailureStaysPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathStatsSummary {
			http.Error(w, "summary down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := newRuntimeStateTestClient(t, srv)
	state, err := client.FetchRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("FetchRuntimeState: %v (partial degradation must not be an error)", err)
	}
	if !state.Partial {
		t.Fatal("state.Partial = false, want true")
	}
}

func TestFetchRuntimeStateNonCoreFailureStaysPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathStatsDcs {
			http.Error(w, "dcs down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := newRuntimeStateTestClient(t, srv)
	state, err := client.FetchRuntimeState(context.Background())
	if err != nil {
		t.Fatalf("FetchRuntimeState: %v", err)
	}
	if !state.Partial {
		t.Fatal("state.Partial = false, want true")
	}
}
