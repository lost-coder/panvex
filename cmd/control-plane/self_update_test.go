package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveSelfUpdateTokenFromTokenFile pins the --token-file resolution
// path: the token is read from disk and whitespace-trimmed.
func TestResolveSelfUpdateTokenFromTokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("file-token-value\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := resolveSelfUpdateToken(tokenSource{
		FilePath: path,
		EnvValue: "env-token-should-not-win",
	})
	if err != nil {
		t.Fatalf("resolveSelfUpdateToken() error = %v", err)
	}
	if got != "file-token-value" {
		t.Fatalf("resolveSelfUpdateToken() = %q, want %q", got, "file-token-value")
	}
}

// TestResolveSelfUpdateTokenFromEnv pins the GITHUB_TOKEN env fallback when
// neither -token nor -token-file is supplied.
func TestResolveSelfUpdateTokenFromEnv(t *testing.T) {
	got, err := resolveSelfUpdateToken(tokenSource{
		EnvValue: "env-token-value",
	})
	if err != nil {
		t.Fatalf("resolveSelfUpdateToken() error = %v", err)
	}
	if got != "env-token-value" {
		t.Fatalf("resolveSelfUpdateToken() = %q, want %q", got, "env-token-value")
	}
}

// TestResolveSelfUpdateTokenBareFlagRejected pins the core 3.2 fix: a bare
// --token flag (no insecure opt-in) must be rejected, since the flag value
// leaks via /proc/<pid>/cmdline to any local user.
func TestResolveSelfUpdateTokenBareFlagRejected(t *testing.T) {
	_, err := resolveSelfUpdateToken(tokenSource{
		FlagValue:  "argv-leaked-token",
		FlagWasSet: true,
		EnvValue:   "env-token-value",
	})
	if err == nil {
		t.Fatal("resolveSelfUpdateToken() error = nil, want errTokenFlagInsecure")
	}
	if !errors.Is(err, errTokenFlagInsecure) {
		t.Fatalf("resolveSelfUpdateToken() error = %v, want errTokenFlagInsecure", err)
	}
}

// TestResolveSelfUpdateTokenFlagAllowedWithEscape pins the explicit
// insecure-allow escape hatch: with AllowInsecure set, the --token flag
// value is honoured (matching the PANVEX_BOOTSTRAP_ALLOW_INSECURE_FLAG
// idiom in bootstrap_admin.go).
func TestResolveSelfUpdateTokenFlagAllowedWithEscape(t *testing.T) {
	got, err := resolveSelfUpdateToken(tokenSource{
		FlagValue:     "argv-token",
		FlagWasSet:    true,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("resolveSelfUpdateToken() error = %v", err)
	}
	if got != "argv-token" {
		t.Fatalf("resolveSelfUpdateToken() = %q, want %q", got, "argv-token")
	}
}

// TestParseSelfUpdateFlagsTokenFlagRejectedByDefault exercises the full flag
// parser: -token without PANVEX_SELF_UPDATE_ALLOW_INSECURE_TOKEN_FLAG must
// error out before self-update proceeds.
func TestParseSelfUpdateFlagsTokenFlagRejectedByDefault(t *testing.T) {
	_, err := parseSelfUpdateFlags([]string{"-token", "argv-leaked-token"})
	if err == nil {
		t.Fatal("parseSelfUpdateFlags() error = nil, want errTokenFlagInsecure")
	}
	if !errors.Is(err, errTokenFlagInsecure) {
		t.Fatalf("parseSelfUpdateFlags() error = %v, want errTokenFlagInsecure", err)
	}
}

// TestParseSelfUpdateFlagsTokenFileResolves exercises the full flag parser
// with -token-file.
func TestParseSelfUpdateFlagsTokenFileResolves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("file-token\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	opts, err := parseSelfUpdateFlags([]string{"-token-file", path})
	if err != nil {
		t.Fatalf("parseSelfUpdateFlags() error = %v", err)
	}
	if opts.token != "file-token" {
		t.Fatalf("opts.token = %q, want %q", opts.token, "file-token")
	}
}

// TestParseSelfUpdateFlagsGitHubTokenEnvResolves exercises the full flag
// parser falling back to the GITHUB_TOKEN env var.
func TestParseSelfUpdateFlagsGitHubTokenEnvResolves(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")

	opts, err := parseSelfUpdateFlags(nil)
	if err != nil {
		t.Fatalf("parseSelfUpdateFlags() error = %v", err)
	}
	if opts.token != "env-token" {
		t.Fatalf("opts.token = %q, want %q", opts.token, "env-token")
	}
}
