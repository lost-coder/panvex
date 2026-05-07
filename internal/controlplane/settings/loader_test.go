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

func TestLoadBootstrap_FromTOML(t *testing.T) {
	in := LoaderInput{
		ConfigPath: "testdata/valid.config.toml",
		Env: []string{
			"PANVEX_ENCRYPTION_KEY=devkey",
		},
	}
	bs, src, err := LoadBootstrap(in)
	if err != nil {
		t.Fatal(err)
	}
	if bs.HTTPListenAddress != "127.0.0.1:9090" {
		t.Errorf("http addr = %q", bs.HTTPListenAddress)
	}
	if bs.StorageDriver != "postgres" {
		t.Errorf("driver = %q", bs.StorageDriver)
	}
	if src["http.listen_address"].Source != SourceConfigFile {
		t.Errorf("http source = %s", src["http.listen_address"].Source)
	}
	if src["http.listen_address"].SourcePath != "testdata/valid.config.toml" {
		t.Errorf("source path = %q", src["http.listen_address"].SourcePath)
	}
}

func TestLoadBootstrap_EnvBeatsTOML(t *testing.T) {
	in := LoaderInput{
		ConfigPath: "testdata/valid.config.toml",
		Env: []string{
			"PANVEX_HTTP_ADDR=:7777",
			"PANVEX_ENCRYPTION_KEY=k",
		},
	}
	bs, src, err := LoadBootstrap(in)
	if err != nil {
		t.Fatal(err)
	}
	if bs.HTTPListenAddress != ":7777" {
		t.Errorf("env should win, got %q", bs.HTTPListenAddress)
	}
	if src["http.listen_address"].Source != SourceEnv {
		t.Errorf("source = %s", src["http.listen_address"].Source)
	}
}

func TestLoadBootstrap_InvalidValueAggregated(t *testing.T) {
	in := LoaderInput{
		Env: []string{
			"PANVEX_STORAGE_DSN=file:./x",
			"PANVEX_ENCRYPTION_KEY=k",
			"PANVEX_HTTP_ADDR=not-a-hostport",
			"PANVEX_TLS_MODE=lol",
		},
	}
	_, _, err := LoadBootstrap(in)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"http.listen_address", "tls.mode"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}
