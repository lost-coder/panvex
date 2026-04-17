package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// stubSessionStore is a minimal in-memory SessionStore used by auth tests to
// exercise the persistent-store code paths without depending on a real
// postgres/sqlite backend.
type stubSessionStore struct {
	mu       sync.Mutex
	sessions map[string]storage.SessionRecord
}

func newStubSessionStore() *stubSessionStore {
	return &stubSessionStore{sessions: make(map[string]storage.SessionRecord)}
}

func (s *stubSessionStore) PutSession(_ context.Context, session storage.SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return nil
}

func (s *stubSessionStore) GetSession(_ context.Context, sessionID string) (storage.SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return storage.SessionRecord{}, storage.ErrNotFound
	}
	return session, nil
}

func (s *stubSessionStore) DeleteSession(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	return nil
}

func (s *stubSessionStore) ListSessions(_ context.Context) ([]storage.SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]storage.SessionRecord, 0, len(s.sessions))
	for _, session := range s.sessions {
		out = append(out, session)
	}
	return out, nil
}

func (s *stubSessionStore) DeleteExpiredSessions(_ context.Context, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, session := range s.sessions {
		if session.CreatedAt.Before(before) {
			delete(s.sessions, id)
		}
	}
	return nil
}

func (s *stubSessionStore) has(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.sessions[sessionID]
	return ok
}

func TestUpdateUserRevokesSessionsOnPasswordChange(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "admin",
		Password: "Admin1password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession() before update error = %v", err)
	}

	_, err = service.UpdateUser(UpdateUserInput{
		UserID:      user.ID,
		Username:    "admin",
		Role:        RoleAdmin,
		NewPassword: "NewAdmin1password",
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err == nil {
		t.Fatal("GetSession() after password change error = nil, want session revoked")
	}
}

func TestUpdateUserRevokesSessionsOnRoleDemotion(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "admin1",
		Password: "Admin1password",
		Role:     RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() first admin error = %v", err)
	}

	// Create a second admin so demotion of the first is allowed.
	_, _, err = service.BootstrapUser(BootstrapInput{
		Username: "admin2",
		Password: "Admin2password",
		Role:     RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() second admin error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "admin1",
		Password: "Admin1password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession() before demotion error = %v", err)
	}

	_, err = service.UpdateUser(UpdateUserInput{
		UserID:   user.ID,
		Username: "admin1",
		Role:     RoleOperator,
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err == nil {
		t.Fatal("GetSession() after role demotion error = nil, want session revoked")
	}
}

func TestUpdateUserKeepsSessionsOnUsernameChange(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "operator",
		Password: "Correct1horse2battery",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	_, err = service.UpdateUser(UpdateUserInput{
		UserID:   user.ID,
		Username: "operator-renamed",
		Role:     RoleOperator,
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession() after username change error = %v, want session preserved", err)
	}
}

// P2-SEC-01: promotions are privilege changes too, so outstanding sessions
// tied to the prior role must be invalidated. This prevents an operator who
// happens to be promoted to admin from continuing to act via a session that
// was issued while they were still a viewer — forcing a fresh login closes
// the audit-trail gap and rotates the session identifier.
func TestUpdateUserRevokesSessionsOnRolePromotion(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "viewer",
		Password: "Viewer1password",
		Role:     RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "viewer",
		Password: "Viewer1password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	_, err = service.UpdateUser(UpdateUserInput{
		UserID:   user.ID,
		Username: "viewer",
		Role:     RoleOperator,
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err == nil {
		t.Fatal("GetSession() after role promotion error = nil, want session revoked")
	}
}

// P2-SEC-01: session fixation. A browser that arrives at the login endpoint
// already carrying a valid panvex_session cookie (e.g. planted by an attacker
// or left over from a terminated session) must not retain that identifier
// after the user successfully authenticates. Authenticate is invoked with
// PriorSessionID set to the existing ID; the old ID must be purged from the
// in-memory session map while a fresh ID is issued.
func TestAuthenticateInvalidatesPriorSessionID(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	if _, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// Simulate an existing pre-auth session (e.g. a stale cookie in the
	// browser or an attacker-planted fixture).
	first, err := service.Authenticate(LoginInput{
		Username: "operator",
		Password: "Correct1horse2battery",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() first error = %v", err)
	}

	if _, err := service.GetSession(first.ID); err != nil {
		t.Fatalf("GetSession() prior session error = %v", err)
	}

	// Re-login carrying the prior session ID as happens when the browser
	// submits the login form with the old cookie still attached.
	second, err := service.Authenticate(LoginInput{
		Username:       "operator",
		Password:       "Correct1horse2battery",
		PriorSessionID: first.ID,
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() second error = %v", err)
	}

	if second.ID == first.ID {
		t.Fatal("Authenticate() returned same session ID as prior session, want rotation")
	}

	if _, err := service.GetSession(first.ID); err == nil {
		t.Fatal("GetSession() prior session after re-login error = nil, want invalidated")
	}

	if _, err := service.GetSession(second.ID); err != nil {
		t.Fatalf("GetSession() new session error = %v, want valid", err)
	}
}

// Defensive guard: PriorSessionID may be empty (first-ever login) or refer to
// an ID that is not currently tracked (e.g. already expired). Neither case
// should abort the login or corrupt other users' sessions.
func TestAuthenticateIgnoresUnknownPriorSessionID(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	if _, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username:       "operator",
		Password:       "Correct1horse2battery",
		PriorSessionID: "not-a-real-session-id",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession() new session error = %v", err)
	}
}

// RevokeSessionsForUser must purge every session belonging to the target
// user while leaving unrelated users' sessions alone.
func TestRevokeSessionsForUserPurgesOnlyTargetUser(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	target, _, err := service.BootstrapUser(BootstrapInput{
		Username: "alice",
		Password: "Alice1password",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser(alice) error = %v", err)
	}

	_, _, err = service.BootstrapUser(BootstrapInput{
		Username: "bob",
		Password: "Bob1password1",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser(bob) error = %v", err)
	}

	alice1, err := service.Authenticate(LoginInput{Username: "alice", Password: "Alice1password"}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(alice 1) error = %v", err)
	}
	alice2, err := service.Authenticate(LoginInput{Username: "alice", Password: "Alice1password"}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(alice 2) error = %v", err)
	}
	bobSession, err := service.Authenticate(LoginInput{Username: "bob", Password: "Bob1password1"}, now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(bob) error = %v", err)
	}

	revoked := service.RevokeSessionsForUser(target.ID)
	if revoked != 2 {
		t.Fatalf("RevokeSessionsForUser() = %d, want %d", revoked, 2)
	}

	if _, err := service.GetSession(alice1.ID); err == nil {
		t.Fatal("GetSession(alice1) after revoke error = nil, want revoked")
	}
	if _, err := service.GetSession(alice2.ID); err == nil {
		t.Fatal("GetSession(alice2) after revoke error = nil, want revoked")
	}
	if _, err := service.GetSession(bobSession.ID); err != nil {
		t.Fatalf("GetSession(bob) error = %v, want preserved", err)
	}
}

// P2-SEC-01 follow-up: after a control-plane restart, s.sessions is empty
// even though the persistent SessionStore still holds the prior session.
// Authenticate must delete the prior ID from the store regardless of whether
// it is tracked in memory; otherwise RestoreSessions on the next restart
// would resurrect an attacker-planted session.
func TestAuthenticatePurgesPriorSessionFromStoreEvenWhenNotInMemory(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	store := newStubSessionStore()
	service.SetSessionStore(store)

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// Simulate a session that survived a control-plane restart: the record
	// exists in the persistent store but the in-memory map is empty (the
	// service was just constructed fresh and RestoreSessions has not been
	// called with this ID yet, or it was evicted some other way).
	priorID := "pre-restart-session-id"
	if err := store.PutSession(context.Background(), storage.SessionRecord{
		ID:        priorID,
		UserID:    user.ID,
		CreatedAt: now.UTC(),
	}); err != nil {
		t.Fatalf("PutSession() error = %v", err)
	}

	// Sanity: in-memory map does not contain the prior session.
	if _, err := service.GetSession(priorID); err == nil {
		t.Fatal("precondition: GetSession(priorID) error = nil, want absent from memory")
	}
	if !store.has(priorID) {
		t.Fatal("precondition: store.has(priorID) = false, want present before login")
	}

	if _, err := service.Authenticate(LoginInput{
		Username:       "operator",
		Password:       "Correct1horse2battery",
		PriorSessionID: priorID,
	}, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if store.has(priorID) {
		t.Fatal("sessionStore still contains priorID after Authenticate, want purged")
	}
}
