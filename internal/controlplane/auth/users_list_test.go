package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestListUsersInMemorySortsAndElides covers the no-store fallback: users are
// returned sorted by CreatedAt (then ID) with password hash and TOTP secret
// zeroed.
func TestListUsersInMemorySortsAndElides(t *testing.T) {
	t.Parallel()
	svc := NewService()
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	svc.LoadUsers([]User{
		{ID: "u-late", Username: "b", Role: RoleAdmin, CreatedAt: late, PasswordHash: "hash2", TotpSecret: "sec2"},
		{ID: "u-early", Username: "a", Role: RoleViewer, CreatedAt: early, PasswordHash: "hash1", TotpSecret: "sec1"},
	})

	users, err := svc.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len = %d, want 2", len(users))
	}
	if users[0].ID != "u-early" || users[1].ID != "u-late" {
		t.Fatalf("not sorted by CreatedAt: %s, %s", users[0].ID, users[1].ID)
	}
	for _, u := range users {
		if u.PasswordHash != "" || u.TotpSecret != "" {
			t.Fatalf("sensitive fields not elided: %+v", u)
		}
	}
}

// TestListUsersFromStoreElidesSensitive covers the persistent path: records are
// mapped to auth.User with the operator-visible fields and no credential
// material.
func TestListUsersFromStoreElidesSensitive(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 15, 8, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	svc := NewServiceWithStore(store)
	if _, _, err := svc.BootstrapUser(context.Background(), BootstrapInput{
		Username: "admin", Password: "Admin1password", Role: RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}
	if _, err := svc.CreateUser(context.Background(), BootstrapInput{
		Username: "viewer", Password: "Viewer1password", Role: RoleViewer,
	}, now.Add(time.Minute)); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	users, err := svc.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len = %d, want 2", len(users))
	}
	names := map[string]User{}
	for _, u := range users {
		if u.PasswordHash != "" || u.TotpSecret != "" {
			t.Fatalf("sensitive fields not elided: %+v", u)
		}
		if u.ID == "" || u.CreatedAt.IsZero() {
			t.Fatalf("operator-visible fields not mapped: %+v", u)
		}
		names[u.Username] = u
	}
	if names["admin"].Role != RoleAdmin || names["viewer"].Role != RoleViewer {
		t.Fatalf("roles not mapped: %+v", names)
	}
}
