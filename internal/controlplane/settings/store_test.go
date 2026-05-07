package settings

import (
	"context"
	"errors"
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
