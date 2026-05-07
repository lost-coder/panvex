package settings

import (
	"strings"
	"testing"
)

func TestLoadBootstrap_DefaultsOnly_FailsForRequiredField(t *testing.T) {
	in := LoaderInput{ConfigPath: "", Env: nil}
	_, _, err := LoadBootstrap(in)
	if err == nil {
		t.Fatal("expected error for missing required fields, got nil")
	}
	for _, want := range []string{"storage.dsn", "auth.encryption_key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("loader error missing %q in:\n%v", want, err)
		}
	}
}

func TestLoadBootstrap_AllFromEnv(t *testing.T) {
	in := LoaderInput{
		ConfigPath: "",
		Env: []string{
			"PANVEX_STORAGE_DSN=file:./x.db",
			"PANVEX_ENCRYPTION_KEY=devkey1234567890",
		},
	}
	bs, src, err := LoadBootstrap(in)
	if err != nil {
		t.Fatal(err)
	}
	if bs.StorageDSN != "file:./x.db" {
		t.Errorf("dsn = %q", bs.StorageDSN)
	}
	if bs.HTTPListenAddress != ":8080" {
		t.Errorf("default http addr lost: %q", bs.HTTPListenAddress)
	}
	if src["storage.dsn"].Source != SourceEnv {
		t.Errorf("dsn source = %s", src["storage.dsn"].Source)
	}
	if src["http.listen_address"].Source != SourceDefault {
		t.Errorf("http.listen_address source = %s", src["http.listen_address"].Source)
	}
}
