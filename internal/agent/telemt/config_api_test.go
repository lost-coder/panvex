package telemt

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetManagedConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/config" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"censorship":{"tls_domain":"a"}},"revision":"rev-9"}`))
	}))
	defer srv.Close()
	c, err := NewClient(Config{BaseURL: srv.URL, Authorization: "test"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	sections, revision, err := c.GetManagedConfig(context.Background())
	if err != nil {
		t.Fatalf("GetManagedConfig: %v", err)
	}
	if revision != "rev-9" {
		t.Fatalf("revision = %q, want rev-9", revision)
	}
	if _, ok := sections["censorship"]; !ok {
		t.Fatalf("expected censorship section, got %+v", sections)
	}
}

func TestHealthReady(t *testing.T) {
	cases := []struct {
		status    int
		body      string
		wantReady bool
	}{
		{http.StatusOK, `{"ok":true,"data":{"ready":true,"status":"ready"}}`, true},
		{http.StatusServiceUnavailable, `{"ok":true,"data":{"ready":false,"reason":"no_healthy_upstreams"}}`, false},
	}
	for _, tc := range cases {
		tc := tc
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		c, err := NewClient(Config{BaseURL: srv.URL, Authorization: "test"}, srv.Client())
		if err != nil {
			srv.Close()
			t.Fatalf("NewClient: %v", err)
		}
		ready, _, err := c.HealthReady(context.Background())
		srv.Close()
		if err != nil {
			t.Fatalf("HealthReady: %v", err)
		}
		if ready != tc.wantReady {
			t.Fatalf("ready = %v, want %v", ready, tc.wantReady)
		}
	}
}

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

func TestPatchConfigReadOnly403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"read_only"}}`))
	}))
	defer srv.Close()
	c, err := NewClient(Config{BaseURL: srv.URL, Authorization: "test-token"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.PatchConfig(context.Background(), map[string]any{"censorship": map[string]any{}}, "")
	if !errors.Is(err, ErrConfigEditReadOnly) {
		t.Fatalf("want ErrConfigEditReadOnly, got %v", err)
	}
}

func TestPatchConfigParsesReloadDecisionFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"data":{"revision":"r2","restart_required":true,` +
			`"runtime_reload_required":true,"process_restart_required":false,` +
			`"deferred_process_fields":[],"changed":["censorship"]}}`))
	}))
	defer srv.Close()
	c, err := NewClient(Config{BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	res, err := c.PatchConfig(context.Background(), map[string]any{"censorship": map[string]any{"tls_domain": "x"}}, "")
	if err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	if res.RuntimeReloadRequired == nil || !*res.RuntimeReloadRequired {
		t.Fatalf("RuntimeReloadRequired = %v, want ptr(true)", res.RuntimeReloadRequired)
	}
	if res.ProcessRestartRequired {
		t.Fatalf("ProcessRestartRequired = true, want false")
	}
}

func TestPatchConfigOldTelemtLeavesReloadFieldNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"data":{"revision":"r2","restart_required":true,"changed":["censorship"]}}`))
	}))
	defer srv.Close()
	c, err := NewClient(Config{BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	res, err := c.PatchConfig(context.Background(), map[string]any{"censorship": map[string]any{"tls_domain": "x"}}, "")
	if err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	if res.RuntimeReloadRequired != nil {
		t.Fatalf("RuntimeReloadRequired = %v, want nil (old Telemt sends no such field)", res.RuntimeReloadRequired)
	}
}

func TestPatchConfigParsesProcessRestartRequiredTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"data":{"revision":"r2","restart_required":true,` +
			`"runtime_reload_required":false,"process_restart_required":true,` +
			`"deferred_process_fields":["listen_port"],"changed":["censorship"]}}`))
	}))
	defer srv.Close()
	c, err := NewClient(Config{BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	res, err := c.PatchConfig(context.Background(), map[string]any{"censorship": map[string]any{"tls_domain": "x"}}, "")
	if err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	if !res.ProcessRestartRequired {
		t.Fatalf("ProcessRestartRequired = false, want true")
	}
	if len(res.DeferredProcessFields) != 1 || res.DeferredProcessFields[0] != "listen_port" {
		t.Fatalf("DeferredProcessFields = %+v", res.DeferredProcessFields)
	}
}

func TestSubmitReloadDrainSendsQueryAndIfMatch(t *testing.T) {
	var gotQuery, gotIfMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotIfMatch = r.Header.Get("If-Match")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true,"data":{"reload_id":7,"target_generation":2,` +
			`"config_revision":"r2","state":"accepted","mode":"drain","failure_policy":"rollback"}}`))
	}))
	defer srv.Close()
	c, err := NewClient(Config{BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	acc, err := c.SubmitReload(context.Background(), "drain", 30, "rollback", "r2")
	if err != nil {
		t.Fatalf("SubmitReload: %v", err)
	}
	if acc.ReloadID != 7 || acc.State != "accepted" {
		t.Fatalf("acc = %+v", acc)
	}
	if gotIfMatch != "r2" {
		t.Fatalf("If-Match = %q, want r2", gotIfMatch)
	}
	// query must carry reload=drain, timeout_secs=30, failure_policy=rollback
	for _, want := range []string{"reload=drain", "timeout_secs=30", "failure_policy=rollback"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestSubmitReloadInProgressMapsToSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"reload_in_progress","message":"Reload 3 is already in progress"}}`))
	}))
	defer srv.Close()
	c, err := NewClient(Config{BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.SubmitReload(context.Background(), "instant", 0, "rollback", "r2")
	if !errors.Is(err, ErrReloadInProgress) {
		t.Fatalf("err = %v, want ErrReloadInProgress", err)
	}
}

func TestGetReloadStatusParsesTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/system/reload/7" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"reload_id":7,"state":"succeeded",` +
			`"finished_at_epoch_secs":100,"deferred_process_fields":[],"warnings":["x"],"error":null}}`))
	}))
	defer srv.Close()
	c, err := NewClient(Config{BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	st, err := c.GetReloadStatus(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetReloadStatus: %v", err)
	}
	if st.State != "succeeded" || len(st.Warnings) != 1 {
		t.Fatalf("st = %+v", st)
	}
}
