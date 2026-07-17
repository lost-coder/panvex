package auth

import (
	"context"
	"testing"
	"time"
)

// TestUpdateUserFailsClosedWhenRevocationFails models the exact scenario the
// PVX-002 fix exists for: an active session, and a store that refuses the
// session delete during a password rotation. D2 mandates revoke-before-
// persist, so a failed revoke must abort the whole update — the stored
// password hash must be UNCHANGED (proven by checking the OLD password
// still verifies directly against the stored hash, not merely by observing
// an error), and the session that could not be revoked must remain valid.
func TestUpdateUserFailsClosedWhenRevocationFails(t *testing.T) {
	now := time.Date(2026, time.July, 17, 11, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	target, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "operator",
		Password: "Operator1password",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(context.Background(), LoginInput{
		Username: "operator",
		Password: "Operator1password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	// The store refuses to delete this session's row, simulating the
	// stolen-session-plus-store-outage scenario from the audit.
	store.failIDs[session.ID] = true

	_, err = service.UpdateUser(context.Background(), UpdateUserInput{
		UserID:      target.ID,
		Username:    target.Username,
		Role:        target.Role,
		NewPassword: "NewOperator1password",
	}, now.Add(2*time.Minute))
	if err == nil {
		t.Fatal("UpdateUser() error = nil, want revoke-before-persist failure")
	}

	// The stored password hash must be UNCHANGED: verify the OLD password
	// still matches the persisted hash directly, not just that an error came
	// back (an error alone would not catch a persist-then-revoke ordering
	// bug that already wrote the new hash before failing).
	stored, err := service.GetUserByID(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if err := service.VerifyPassword(stored.PasswordHash, "Operator1password"); err != nil {
		t.Fatalf("VerifyPassword(old password) after failed UpdateUser = %v, want still valid — credential must not change when revoke fails", err)
	}
	if err := service.VerifyPassword(stored.PasswordHash, "NewOperator1password"); err == nil {
		t.Fatal("VerifyPassword(new password) after failed UpdateUser = nil error, want rejected — new password must never have been persisted")
	}

	// The session whose revoke failed must still be usable — the operation
	// never got far enough to touch it in a way that would leave it in an
	// inconsistent state.
	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession(session) after failed UpdateUser = %v, want still valid", err)
	}
}

// TestUpdateUserRevokesBeforePersistOnSuccess is the happy path: revocation
// succeeds for every session except the caller's own (ExceptSessionID), and
// only then is the new password persisted.
func TestUpdateUserRevokesBeforePersistOnSuccess(t *testing.T) {
	now := time.Date(2026, time.July, 17, 11, 30, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	target, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "operator",
		Password: "Operator1password",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	callerSession, err := service.Authenticate(context.Background(), LoginInput{
		Username: "operator",
		Password: "Operator1password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(caller) error = %v", err)
	}
	otherSession, err := service.Authenticate(context.Background(), LoginInput{
		Username: "operator",
		Password: "Operator1password",
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(other) error = %v", err)
	}

	updated, err := service.UpdateUser(context.Background(), UpdateUserInput{
		UserID:          target.ID,
		Username:        target.Username,
		Role:            target.Role,
		NewPassword:     "NewOperator1password",
		ExceptSessionID: callerSession.ID,
	}, now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	if _, err := service.GetSession(callerSession.ID); err != nil {
		t.Fatalf("GetSession(caller) after successful UpdateUser = %v, want preserved (ExceptSessionID)", err)
	}
	if _, err := service.GetSession(otherSession.ID); err == nil {
		t.Fatal("GetSession(other) after successful UpdateUser = nil error, want revoked")
	}

	if _, err := service.Authenticate(context.Background(), LoginInput{
		Username: updated.Username,
		Password: "NewOperator1password",
	}, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("Authenticate(new password) error = %v, want success", err)
	}
	if _, err := service.Authenticate(context.Background(), LoginInput{
		Username: updated.Username,
		Password: "Operator1password",
	}, now.Add(5*time.Minute)); err == nil {
		t.Fatal("Authenticate(old password) after successful rotation = nil error, want rejected")
	}
}
