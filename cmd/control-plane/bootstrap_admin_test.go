package main

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestBootstrapAdmin_RejectsPasswordFlagInNonInteractive(t *testing.T) {
	t.Setenv("PANVEX_BOOTSTRAP_ALLOW_INSECURE_FLAG", "")
	err := validatePasswordSource(passwordSource{
		FlagValue:     "secret",
		FlagWasSet:    true,
		FilePath:      "",
		StdinIsTTY:    false,
		AllowInsecure: false,
	})
	if err == nil {
		t.Fatalf("expected rejection of -password in non-interactive mode")
	}
	if !errors.Is(err, errPasswordFlagInsecure) {
		t.Fatalf("expected errPasswordFlagInsecure, got %v", err)
	}
}

func TestBootstrapAdmin_AllowsPasswordFile(t *testing.T) {
	err := validatePasswordSource(passwordSource{
		FilePath:   "/run/secrets/admin",
		StdinIsTTY: false,
	})
	if err != nil {
		t.Fatalf("expected -password-file path to be accepted, got %v", err)
	}
}

func TestBootstrapAdmin_AllowsTTYPrompt(t *testing.T) {
	err := validatePasswordSource(passwordSource{StdinIsTTY: true})
	if err != nil {
		t.Fatalf("expected TTY-prompt mode to be accepted, got %v", err)
	}
}

func TestBootstrapAdmin_AllowsExplicitOverride(t *testing.T) {
	err := validatePasswordSource(passwordSource{
		FlagValue:     "secret",
		FlagWasSet:    true,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("expected explicit override to be accepted, got %v", err)
	}
}

// TestRunBootstrapAdmin_RejectsInsecurePasswordFlag verifies the validation
// is wired into runBootstrapAdmin and aborts before any storage is opened or
// any user is created. Tests run with stdin detached from a TTY, so the
// -password flag must be rejected.
func TestRunBootstrapAdmin_RejectsInsecurePasswordFlag(t *testing.T) {
	t.Setenv("PANVEX_BOOTSTRAP_ALLOW_INSECURE_FLAG", "")
	t.Setenv("PANVEX_BOOTSTRAP_PASSWORD", "")
	t.Setenv("PANVEX_BOOTSTRAP_PASSWORD_FILE", "")

	err := runBootstrapAdmin([]string{
		"-username", "root",
		"-password", "leak",
		"-storage-driver", "sqlite",
		"-storage-dsn", filepath.Join(t.TempDir(), "test.db"),
	})
	if !errors.Is(err, errPasswordFlagInsecure) {
		t.Fatalf("expected errPasswordFlagInsecure, got %v", err)
	}
}
