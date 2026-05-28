package settings

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeReader struct {
	panel   map[string]string // column -> raw text
	runtime map[string]string // name -> json text
}

func (f *fakeReader) ReadPanelColumn(_ context.Context, col string) (string, error) {
	v, ok := f.panel[col]
	if !ok {
		return "", errors.New("not set")
	}
	return v, nil
}

func (f *fakeReader) ReadRuntimeSetting(_ context.Context, name string) (string, time.Time, string, error) {
	v, ok := f.runtime[name]
	if !ok {
		return "", time.Time{}, "", errors.New("not set")
	}
	return v, time.Unix(0, 0), "", nil
}

func TestOperationalStore_LoadFromMixedSources(t *testing.T) {
	r := &fakeReader{
		panel: map[string]string{
			"password_min_length":  "12",
			"http_public_url":      "https://panel.example",
			"grpc_public_endpoint": "panel.example:8443",
			"retention_json":       `{"audit_days":30}`,
			"geoip_json":           `{"mode":"off"}`,
		},
		runtime: map[string]string{
			"updates.channel":          `"stable"`,
			"updates.allow_prerelease": `false`,
		},
	}
	store := NewOperationalStore(r)
	if err := store.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.PasswordMinLength(); got != 12 {
		t.Errorf("PasswordMinLength = %d", got)
	}
	if got := store.HTTPPublicURL(); got != "https://panel.example" {
		t.Errorf("HTTPPublicURL = %q", got)
	}
	if got := store.UpdatesChannel(); got != "stable" {
		t.Errorf("UpdatesChannel = %q", got)
	}
}

type fakeWriter struct {
	r      *fakeReader
	writes int
}

func newFakeWriter(r *fakeReader) *fakeWriter { return &fakeWriter{r: r} }

func (w *fakeWriter) WritePanelColumn(_ context.Context, col, raw, _ string) error {
	if w.r.panel == nil {
		w.r.panel = map[string]string{}
	}
	w.r.panel[col] = raw
	w.writes++
	return nil
}
func (w *fakeWriter) WriteRuntimeSetting(_ context.Context, name, valueJSON, _ string) error {
	if w.r.runtime == nil {
		w.r.runtime = map[string]string{}
	}
	w.r.runtime[name] = valueJSON
	w.writes++
	return nil
}

func TestOperationalStore_PutThenReload(t *testing.T) {
	r := &fakeReader{
		panel:   map[string]string{"password_min_length": "10"},
		runtime: map[string]string{},
	}
	w := newFakeWriter(r)
	s := NewOperationalStoreRW(r, w)
	if err := s.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	updates := map[string]string{
		"auth.password_min_length": "16",
		"updates.channel":          "beta",
	}
	if err := s.Put(context.Background(), updates, "tester"); err != nil {
		t.Fatal(err)
	}
	if r.panel["password_min_length"] != "16" {
		t.Errorf("panel write missed: %q", r.panel["password_min_length"])
	}
	if r.runtime["updates.channel"] != `"beta"` {
		t.Errorf("runtime write missed: %q", r.runtime["updates.channel"])
	}
	if s.PasswordMinLength() != 16 {
		t.Errorf("cache not updated after Put: %d", s.PasswordMinLength())
	}
}

func TestOperationalStore_PutRejectsBootstrap(t *testing.T) {
	r := &fakeReader{panel: map[string]string{}, runtime: map[string]string{}}
	w := newFakeWriter(r)
	s := NewOperationalStoreRW(r, w)
	if err := s.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := s.Put(context.Background(), map[string]string{
		"http.listen_address": ":7777",
	}, "tester")
	if err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("expected bootstrap rejection, got %v", err)
	}
}

func TestOperationalStore_PutValidates(t *testing.T) {
	r := &fakeReader{panel: map[string]string{"password_min_length": "10"}}
	w := newFakeWriter(r)
	s := NewOperationalStoreRW(r, w)
	_ = s.Reload(context.Background())
	err := s.Put(context.Background(), map[string]string{
		"auth.password_min_length": "3",
	}, "tester")
	if err == nil || !strings.Contains(err.Error(), "below min") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestPendingChangesUsesApplyTier(t *testing.T) {
	r := &fakeReader{panel: map[string]string{}, runtime: map[string]string{}}
	s := NewOperationalStore(r)
	// Seed the cached snapshot with a restart-tier and a live-tier field.
	s.cache.Store(&snapshot{values: map[string]string{
		"auth.session_idle_timeout": "30m",
		"http.public_url":           "https://a.example",
	}})
	active := s.CaptureActive()

	// Mutate BOTH fields in the live cache.
	s.cache.Store(&snapshot{values: map[string]string{
		"auth.session_idle_timeout": "45m",
		"http.public_url":           "https://b.example",
	}})

	got := s.PendingChanges(active)
	if len(got) != 1 || got[0] != "auth.session_idle_timeout" {
		t.Fatalf("PendingChanges = %v, want only [auth.session_idle_timeout]", got)
	}
}

func TestOperationalStore_DurationGettersFallBackToDefault(t *testing.T) {
	r := &fakeReader{panel: map[string]string{}, runtime: map[string]string{}}
	s := NewOperationalStore(r)
	if err := s.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.MetricsPollInterval(); got != 5*time.Second {
		t.Errorf("got %v, want 5s default", got)
	}
	if got := s.AuthPasswordLockoutMaxAttempts(); got != 5 {
		t.Errorf("got %d, want 5 default", got)
	}
}

func TestSeedDefaultsNoOpWithoutSeedSources(t *testing.T) {
	r := &fakeReader{panel: map[string]string{}, runtime: map[string]string{}}
	w := newFakeWriter(r)
	s := NewOperationalStoreRW(r, w)
	if err := s.SeedDefaults(context.Background(), LoaderInput{Env: []string{"PANVEX_HTTP_ADDR=:9090"}}); err != nil {
		t.Fatal(err)
	}
	if w.writes != 0 {
		t.Errorf("writes = %d, want 0", w.writes)
	}
}

func TestSeedDefaultsSkipsAlreadyStored(t *testing.T) {
	r := &fakeReader{panel: map[string]string{}, runtime: map[string]string{}}
	r.panel["http_public_url"] = "https://already.example"
	w := newFakeWriter(r)
	s := NewOperationalStoreRW(r, w)
	if err := s.SeedDefaults(context.Background(), LoaderInput{}); err != nil {
		t.Fatal(err)
	}
	if w.writes != 0 {
		t.Errorf("writes = %d, want 0", w.writes)
	}
}

func TestReloadTracksSource(t *testing.T) {
	r := &fakeReader{panel: map[string]string{}, runtime: map[string]string{}}
	r.panel["http_public_url"] = "https://set.example"
	s := NewOperationalStore(r)
	if err := s.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.Source("http.public_url"); got != SourceDB {
		t.Errorf("Source(http.public_url) = %q, want %q", got, SourceDB)
	}
	if got := s.Source("grpc.public_endpoint"); got != SourceDefault {
		t.Errorf("Source(grpc.public_endpoint) = %q, want %q", got, SourceDefault)
	}
	if s.OverriddenByEnv("http.public_url") {
		t.Errorf("OverriddenByEnv(http.public_url) = true, want false")
	}
}

func TestOperationalStore_DurationGettersUseStoredValue(t *testing.T) {
	r := &fakeReader{
		panel:   map[string]string{},
		runtime: map[string]string{"observability.metrics_poll_interval": `"15s"`},
	}
	s := NewOperationalStore(r)
	if err := s.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.MetricsPollInterval(); got != 15*time.Second {
		t.Errorf("got %v, want 15s", got)
	}
}
