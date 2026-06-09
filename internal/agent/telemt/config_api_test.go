package telemt

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPatchConfigSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/config" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("If-Match"); got != "rev-1" {
			t.Errorf("If-Match = %q, want rev-1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"revision":"rev-2","restart_required":true,"changed":["censorship"]}}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{BaseURL: srv.URL, Authorization: "test-token"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	res, err := c.PatchConfig(context.Background(), map[string]any{"censorship": map[string]any{"tls_domain": "b"}}, "rev-1")
	if err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	if res.Revision != "rev-2" || !res.RestartRequired || len(res.Changed) != 1 || res.Changed[0] != "censorship" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestPatchConfigUnsupported404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, err := NewClient(Config{BaseURL: srv.URL, Authorization: "test-token"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.PatchConfig(context.Background(), map[string]any{"censorship": map[string]any{}}, "")
	if !errors.Is(err, ErrConfigEditUnsupported) {
		t.Fatalf("want ErrConfigEditUnsupported, got %v", err)
	}
}

func TestPatchConfigRevisionConflict409(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"revision_conflict","message":"mismatch"}}`))
	}))
	defer srv.Close()

	c, err := NewClient(Config{BaseURL: srv.URL, Authorization: "test-token"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.PatchConfig(context.Background(), map[string]any{"censorship": map[string]any{}}, "stale")
	if !errors.Is(err, ErrConfigRevisionConflict) {
		t.Fatalf("want ErrConfigRevisionConflict, got %v", err)
	}
}
