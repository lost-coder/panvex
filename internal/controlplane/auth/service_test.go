package auth

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestServiceAuthenticateRejectsOperatorWithoutTotp(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	user, secret, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "correct horse battery staple",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	if secret == "" {
		t.Fatal("BootstrapUser() secret = empty, want seeded TOTP secret")
	}

	_, err = service.Authenticate(LoginInput{
		Username: "operator",
		Password: "correct horse battery staple",
	}, now.Add(30*time.Second))
	if err == nil {
		t.Fatal("Authenticate() error = nil, want TOTP requirement failure")
	}

	if err != ErrTotpRequired {
		t.Fatalf("Authenticate() error = %v, want %v", err, ErrTotpRequired)
	}

	code, err := service.GenerateTotpCode(secret, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "operator",
		Password: "correct horse battery staple",
		TotpCode: code,
	}, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if session.UserID != user.ID {
		t.Fatalf("session.UserID = %q, want %q", session.UserID, user.ID)
	}
}

func TestServiceAuthenticateAllowsViewerWithoutTotp(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "viewer",
		Password: "viewer-password",
		Role:     RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "viewer",
		Password: "viewer-password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if session.UserID != user.ID {
		t.Fatalf("session.UserID = %q, want %q", session.UserID, user.ID)
	}
}

func TestServiceHashAndVerifyPassword(t *testing.T) {
	service := NewService()

	hash, err := service.HashPassword("ultra-secret")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if hash == "ultra-secret" {
		t.Fatal("HashPassword() returned plaintext password")
	}

	if err := service.VerifyPassword(hash, "ultra-secret"); err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
}

func TestServiceSessionLifecycle(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "viewer",
		Password: "viewer-password",
		Role:     RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "viewer",
		Password: "viewer-password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	stored, err := service.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}

	if stored.UserID != user.ID {
		t.Fatalf("stored.UserID = %q, want %q", stored.UserID, user.ID)
	}

	if err := service.Logout(session.ID); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err == nil {
		t.Fatal("GetSession() after logout error = nil, want not found")
	}
}

func TestServiceSnapshotAndLoadUsers(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	user, secret, err := service.BootstrapUser(BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	snapshot := service.SnapshotUsers()
	if len(snapshot) != 1 {
		t.Fatalf("len(SnapshotUsers()) = %d, want %d", len(snapshot), 1)
	}

	restored := NewService()
	restored.LoadUsers(snapshot)

	code, err := restored.GenerateTotpCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}

	session, err := restored.Authenticate(LoginInput{
		Username: "admin",
		Password: "admin-password",
		TotpCode: code,
	}, now)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if session.UserID != user.ID {
		t.Fatalf("session.UserID = %q, want %q", session.UserID, user.ID)
	}
}

func TestServiceBootstrapUserPersistsThroughStore(t *testing.T) {
	now := time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	service := NewServiceWithStore(store)
	user, secret, err := service.BootstrapUser(BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	restored := NewServiceWithStore(store)
	code, err := restored.GenerateTotpCode(secret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}

	session, err := restored.Authenticate(LoginInput{
		Username: "admin",
		Password: "admin-password",
		TotpCode: code,
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if session.UserID != user.ID {
		t.Fatalf("session.UserID = %q, want %q", session.UserID, user.ID)
	}
}
