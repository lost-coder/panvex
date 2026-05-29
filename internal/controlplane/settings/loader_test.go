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
	if bs.ObservabilityLogLevel != "info" {
		t.Errorf("default log level lost: %q", bs.ObservabilityLogLevel)
	}
	if src["storage.dsn"].Source != SourceEnv {
		t.Errorf("dsn source = %s", src["storage.dsn"].Source)
	}
	if src["observability.log_level"].Source != SourceDefault {
		t.Errorf("observability.log_level source = %s", src["observability.log_level"].Source)
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
	if bs.ObservabilityLogLevel != "warn" {
		t.Errorf("log level = %q", bs.ObservabilityLogLevel)
	}
	if bs.StorageDriver != "postgres" {
		t.Errorf("driver = %q", bs.StorageDriver)
	}
	if src["observability.log_level"].Source != SourceConfigFile {
		t.Errorf("log level source = %s", src["observability.log_level"].Source)
	}
	if src["observability.log_level"].SourcePath != "testdata/valid.config.toml" {
		t.Errorf("source path = %q", src["observability.log_level"].SourcePath)
	}
}

func TestLoadBootstrap_EnvBeatsTOML(t *testing.T) {
	in := LoaderInput{
		ConfigPath: "testdata/valid.config.toml",
		Env: []string{
			"PANVEX_LOG_LEVEL=error",
			"PANVEX_ENCRYPTION_KEY=k",
		},
	}
	bs, src, err := LoadBootstrap(in)
	if err != nil {
		t.Fatal(err)
	}
	if bs.ObservabilityLogLevel != "error" {
		t.Errorf("env should win, got %q", bs.ObservabilityLogLevel)
	}
	if src["observability.log_level"].Source != SourceEnv {
		t.Errorf("source = %s", src["observability.log_level"].Source)
	}
}

func TestLoadBootstrap_InvalidValueAggregated(t *testing.T) {
	in := LoaderInput{
		Env: []string{
			"PANVEX_STORAGE_DSN=file:./x",
			"PANVEX_ENCRYPTION_KEY=k",
			"PANVEX_LOG_LEVEL=not-a-level",
			"PANVEX_TLS_MODE=lol",
		},
	}
	_, _, err := LoadBootstrap(in)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"observability.log_level", "tls.mode"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}
