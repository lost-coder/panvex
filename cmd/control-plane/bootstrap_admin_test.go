package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/kdf"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
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

// TestRunBootstrapAdminHonoursKDFProfile: bootstrap-admin hashes the very first
// password on the box, so it must honour PANVEX_KDF_PROFILE like serve does.
// Otherwise a low-memory install gets a 96 MiB default-profile hash written by
// the CLI, and the operator's first login pays that 96 MiB derivation on the
// exact hardware the profile exists to protect.
func TestRunBootstrapAdminHonoursKDFProfile(t *testing.T) {
	t.Setenv("PANVEX_BOOTSTRAP_ALLOW_INSECURE_FLAG", "1")
	t.Setenv("PANVEX_KDF_PROFILE", "low-memory")
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })

	databasePath := filepath.Join(t.TempDir(), "panvex.db")
	if err := runBootstrapAdmin([]string{
		"-storage-driver", "sqlite",
		"-storage-dsn", databasePath,
		"-username", "admin",
		"-password", "StrongPassword123!",
	}); err != nil {
		t.Fatalf("runBootstrapAdmin() error = %v", err)
	}

	store, err := sqlite.Open(databasePath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	user, err := store.GetUserByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	const wantPrefix = "argon2id$v=3$t=2,m=19456,p=1$"
	if !strings.HasPrefix(user.PasswordHash, wantPrefix) {
		t.Fatalf("bootstrap hash = %q, want prefix %q", user.PasswordHash, wantPrefix)
	}
}

// TestRunBootstrapAdminRejectsUnknownKDFProfile: a typo must fail the command
// loudly rather than silently writing a hash at a different cost.
func TestRunBootstrapAdminRejectsUnknownKDFProfile(t *testing.T) {
	t.Setenv("PANVEX_BOOTSTRAP_ALLOW_INSECURE_FLAG", "1")
	t.Setenv("PANVEX_KDF_PROFILE", "lowmemory")
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })

	err := runBootstrapAdmin([]string{
		"-storage-driver", "sqlite",
		"-storage-dsn", filepath.Join(t.TempDir(), "panvex.db"),
		"-username", "admin",
		"-password", "StrongPassword123!",
	})
	if err == nil {
		t.Fatal("unknown PANVEX_KDF_PROFILE must fail the command")
	}
	if !strings.Contains(err.Error(), "PANVEX_KDF_PROFILE") {
		t.Fatalf("error must name the offending env var, got %v", err)
	}
}
