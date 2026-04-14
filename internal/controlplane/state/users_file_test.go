package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
)

func TestSaveAndLoadUsersFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth-state.json")
	users := []auth.User{
		{
			ID:           "user-000001",
			Username:     "admin",
			PasswordHash: "argon2id$abc$def",
			Role:         auth.RoleAdmin,
			TotpSecret:   "SECRET",
			CreatedAt:    time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC),
		},
	}

	if err := SaveUsersFile(path, users); err != nil {
		t.Fatalf("SaveUsersFile() error = %v", err)
	}

	loaded, err := LoadUsersFile(path)
	if err != nil {
		t.Fatalf("LoadUsersFile() error = %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("len(loaded) = %d, want %d", len(loaded), 1)
	}

	if loaded[0].Username != "admin" {
		t.Fatalf("loaded[0].Username = %q, want %q", loaded[0].Username, "admin")
	}
}
