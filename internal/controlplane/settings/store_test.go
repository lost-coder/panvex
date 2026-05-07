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

type fakeWriter struct{ r *fakeReader }

func newFakeWriter(r *fakeReader) *fakeWriter { return &fakeWriter{r: r} }

func (w *fakeWriter) WritePanelColumn(_ context.Context, col, raw, _ string) error {
	if w.r.panel == nil {
		w.r.panel = map[string]string{}
	}
	w.r.panel[col] = raw
	return nil
}
func (w *fakeWriter) WriteRuntimeSetting(_ context.Context, name, valueJSON, _ string) error {
	if w.r.runtime == nil {
		w.r.runtime = map[string]string{}
	}
	w.r.runtime[name] = valueJSON
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
