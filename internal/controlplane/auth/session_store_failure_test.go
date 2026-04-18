package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// brokenSessionStore satisfies storage.SessionStore but fails PutSession to
// exercise the P2-SEC-07 error-propagation path in Authenticate.
type brokenSessionStore struct {
	putErr error
	// putCount counts PutSession invocations so tests can verify the path
	// was actually taken.
	putCount int
}

func (s *brokenSessionStore) PutSession(_ context.Context, _ storage.SessionRecord) error {
	s.putCount++
	return s.putErr
}

func (s *brokenSessionStore) GetSession(_ context.Context, _ string) (storage.SessionRecord, error) {
	return storage.SessionRecord{}, storage.ErrNotFound
}

func (s *brokenSessionStore) DeleteSession(_ context.Context, _ string) error {
	return nil
}

func (s *brokenSessionStore) ListSessions(_ context.Context) ([]storage.SessionRecord, error) {
	return nil, nil
}

func (s *brokenSessionStore) DeleteExpiredSessions(_ context.Context, _ time.Time) error {
	return nil
}

// TestLoginFailsWhenSessionStoreBroken verifies P2-SEC-07: when the
// persistent session store rejects PutSession, Authenticate must return
// ErrSessionStoreUnavailable and must NOT create an in-memory session.
func TestLoginFailsWhenSessionStoreBroken(t *testing.T) {
	now := time.Date(2026, time.April, 17, 10, 0, 0, 0, time.UTC)
	service := NewService()

	_, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}

	storeErr := errors.New("simulated disk-full")
	broken := &brokenSessionStore{putErr: storeErr}
	service.SetSessionStore(broken)

	session, err := service.Authenticate(LoginInput{
		Username: "operator",
		Password: "Correct1horse2battery",
	}, now.Add(time.Minute))
	if err == nil {
		t.Fatal("Authenticate() error = nil, want ErrSessionStoreUnavailable")
	}
	if !errors.Is(err, ErrSessionStoreUnavailable) {
		t.Fatalf("Authenticate() error = %v, want wrapping ErrSessionStoreUnavailable", err)
	}
	if session.ID != "" {
		t.Fatalf("Authenticate() returned session with ID %q on failure, want zero value", session.ID)
	}
	if broken.putCount != 1 {
		t.Fatalf("PutSession call count = %d, want 1", broken.putCount)
	}

	// Critical: no in-memory session must have been created — otherwise the
	// operator would appear logged in until the next CP restart.
	service.mu.RLock()
	count := len(service.sessions)
	service.mu.RUnlock()
	if count != 0 {
		t.Fatalf("in-memory session count = %d, want 0 on store failure", count)
	}
}

// TestLogoutToleratesBrokenSessionStore verifies that a DeleteSession error
// does not block logout — the in-memory session is still revoked and the
// error is only logged (P2-SEC-07).
func TestLogoutToleratesBrokenSessionStore(t *testing.T) {
	now := time.Date(2026, time.April, 17, 10, 0, 0, 0, time.UTC)
	service := NewService()
	// Pin the service clock so the Logout path's internal expiry sweep uses
	// the same frame as the session we are about to create. Without this,
	// wall-clock time.Now() (which drifts past the fixed `now` in tests run
	// even a day later) would expire the session before Logout inspects the
	// in-memory map and cause it to return ErrSessionNotFound.
	service.SetNow(func() time.Time { return now.Add(2 * time.Minute) })

	_, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}

	// First login while the store is healthy.
	good := &brokenSessionStore{putErr: nil}
	service.SetSessionStore(good)
	session, err := service.Authenticate(LoginInput{
		Username: "operator",
		Password: "Correct1horse2battery",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Now swap in a store that fails DeleteSession. The in-memory map still
	// drops the session and the caller sees success.
	service.SetSessionStore(&failingDeleteStore{delErr: errors.New("delete broken")})
	if err := service.Logout(session.ID); err != nil {
		t.Fatalf("Logout() error = %v, want nil (tolerated)", err)
	}

	service.mu.RLock()
	_, stillThere := service.sessions[session.ID]
	service.mu.RUnlock()
	if stillThere {
		t.Fatal("session still present after Logout() with broken delete")
	}
}

type failingDeleteStore struct {
	brokenSessionStore
	delErr error
}

func (f *failingDeleteStore) DeleteSession(_ context.Context, _ string) error {
	return f.delErr
}
