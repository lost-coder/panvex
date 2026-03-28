package auth

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestServiceBootstrapUserLeavesTotpDisabledByDefault(t *testing.T) {
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

	if secret != "" {
		t.Fatalf("BootstrapUser() secret = %q, want empty", secret)
	}

	if user.TotpEnabled {
		t.Fatal("BootstrapUser() TotpEnabled = true, want false")
	}

	if user.TotpSecret != "" {
		t.Fatalf("BootstrapUser() TotpSecret = %q, want empty", user.TotpSecret)
	}
}

func TestServiceAuthenticateAllowsOperatorWithoutTotpWhenDisabled(t *testing.T) {
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

	if secret != "" {
		t.Fatalf("BootstrapUser() secret = %q, want empty", secret)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "operator",
		Password: "correct horse battery staple",
	}, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if session.UserID != user.ID {
		t.Fatalf("session.UserID = %q, want %q", session.UserID, user.ID)
	}
}

func TestServiceEnableTotpRequiresPendingSetup(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "correct horse battery staple",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	_, err = service.EnableTotp(user.ID, "correct horse battery staple", "123456", now.Add(30*time.Second))
	if err == nil {
		t.Fatal("EnableTotp() error = nil, want pending setup failure")
	}

	if err != ErrTotpSetupNotFound {
		t.Fatalf("EnableTotp() error = %v, want %v", err, ErrTotpSetupNotFound)
	}
}

func TestServiceEnableTotpRequiresValidPasswordAndCode(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "correct horse battery staple",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	secret, err := service.StartTotpSetup(user.ID, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("StartTotpSetup() error = %v", err)
	}

	code, err := service.GenerateTotpCode(secret, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}

	_, err = service.EnableTotp(user.ID, "wrong-password", code, now.Add(30*time.Second))
	if err == nil {
		t.Fatal("EnableTotp() error = nil, want password validation failure")
	}

	if err != ErrInvalidCredentials {
		t.Fatalf("EnableTotp() error = %v, want %v", err, ErrInvalidCredentials)
	}

	_, err = service.EnableTotp(user.ID, "correct horse battery staple", "000000", now.Add(30*time.Second))
	if err == nil {
		t.Fatal("EnableTotp() error = nil, want TOTP validation failure")
	}

	if err != ErrInvalidTotpCode {
		t.Fatalf("EnableTotp() error = %v, want %v", err, ErrInvalidTotpCode)
	}

	enabledUser, err := service.EnableTotp(user.ID, "correct horse battery staple", code, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("EnableTotp() error = %v", err)
	}

	if !enabledUser.TotpEnabled {
		t.Fatal("EnableTotp() TotpEnabled = false, want true")
	}

	if enabledUser.TotpSecret != secret {
		t.Fatalf("EnableTotp() TotpSecret = %q, want %q", enabledUser.TotpSecret, secret)
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

func TestServiceDisableTotpRequiresValidPasswordAndCode(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "correct horse battery staple",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	secret, err := service.StartTotpSetup(user.ID, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("StartTotpSetup() error = %v", err)
	}

	code, err := service.GenerateTotpCode(secret, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}

	if _, err := service.EnableTotp(user.ID, "correct horse battery staple", code, now.Add(30*time.Second)); err != nil {
		t.Fatalf("EnableTotp() error = %v", err)
	}

	currentCode, err := service.GenerateTotpCode(secret, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("GenerateTotpCode() current error = %v", err)
	}

	_, err = service.DisableTotp(user.ID, "wrong-password", currentCode, now.Add(time.Minute))
	if err == nil {
		t.Fatal("DisableTotp() error = nil, want password validation failure")
	}

	if err != ErrInvalidCredentials {
		t.Fatalf("DisableTotp() error = %v, want %v", err, ErrInvalidCredentials)
	}

	_, err = service.DisableTotp(user.ID, "correct horse battery staple", "000000", now.Add(time.Minute))
	if err == nil {
		t.Fatal("DisableTotp() error = nil, want TOTP validation failure")
	}

	if err != ErrInvalidTotpCode {
		t.Fatalf("DisableTotp() error = %v, want %v", err, ErrInvalidTotpCode)
	}

	disabledUser, err := service.DisableTotp(user.ID, "correct horse battery staple", currentCode, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("DisableTotp() error = %v", err)
	}

	if disabledUser.TotpEnabled {
		t.Fatal("DisableTotp() TotpEnabled = true, want false")
	}

	if disabledUser.TotpSecret != "" {
		t.Fatalf("DisableTotp() TotpSecret = %q, want empty", disabledUser.TotpSecret)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "operator",
		Password: "correct horse battery staple",
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() after disable error = %v", err)
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
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

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

func TestServiceGetSessionPrunesOtherExpiredSessions(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

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
	}, now)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	service.mu.Lock()
	service.sessions["session-expired"] = Session{
		ID:        "session-expired",
		UserID:    user.ID,
		CreatedAt: now.Add(-sessionTTL - time.Minute),
	}
	service.mu.Unlock()

	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}

	service.mu.RLock()
	_, stillPresent := service.sessions["session-expired"]
	service.mu.RUnlock()
	if stillPresent {
		t.Fatal("expired session still present after GetSession()")
	}
}

func TestServiceSnapshotAndLoadUsers(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	user, _, err := service.BootstrapUser(BootstrapInput{
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

	if snapshot[0].TotpEnabled {
		t.Fatal("SnapshotUsers()[0].TotpEnabled = true, want false")
	}

	session, err := restored.Authenticate(LoginInput{
		Username: "admin",
		Password: "admin-password",
	}, now)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if session.UserID != user.ID {
		t.Fatalf("session.UserID = %q, want %q", session.UserID, user.ID)
	}
}

func TestServiceResetTotpClearsEnabledState(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "correct horse battery staple",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	secret, err := service.StartTotpSetup(user.ID, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("StartTotpSetup() error = %v", err)
	}

	code, err := service.GenerateTotpCode(secret, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}

	if _, err := service.EnableTotp(user.ID, "correct horse battery staple", code, now.Add(30*time.Second)); err != nil {
		t.Fatalf("EnableTotp() error = %v", err)
	}

	resetUser, err := service.ResetTotp(user.ID)
	if err != nil {
		t.Fatalf("ResetTotp() error = %v", err)
	}

	if resetUser.TotpEnabled {
		t.Fatal("ResetTotp() TotpEnabled = true, want false")
	}

	if resetUser.TotpSecret != "" {
		t.Fatalf("ResetTotp() TotpSecret = %q, want empty", resetUser.TotpSecret)
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

	if secret != "" {
		t.Fatalf("BootstrapUser() secret = %q, want empty", secret)
	}

	if user.TotpEnabled {
		t.Fatal("BootstrapUser() TotpEnabled = true, want false")
	}

	restored := NewServiceWithStore(store)
	session, err := restored.Authenticate(LoginInput{
		Username: "admin",
		Password: "admin-password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if session.UserID != user.ID {
		t.Fatalf("session.UserID = %q, want %q", session.UserID, user.ID)
	}
}
