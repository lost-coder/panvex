package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// selectiveFailSessionStore is a minimal in-memory SessionStore fake that
// actually retains PutSession'd records — unlike brokenSessionStore /
// failingDeleteStore in session_store_failure_test.go, which are stateless
// stubs. RevokeSessionsForUserExcept's fail-closed guarantee (D1) is about
// whether a row SURVIVES in the store after a failed delete, so the fake
// must model real persistence for that to be provable. DeleteSession fails
// only for IDs listed in failIDs; everything else behaves like a normal
// store.
type selectiveFailSessionStore struct {
	records     map[string]storage.SessionRecord
	failIDs     map[string]bool
	notFoundIDs map[string]bool
}

func newSelectiveFailSessionStore() *selectiveFailSessionStore {
	return &selectiveFailSessionStore{
		records:     make(map[string]storage.SessionRecord),
		failIDs:     make(map[string]bool),
		notFoundIDs: make(map[string]bool),
	}
}

func (f *selectiveFailSessionStore) PutSession(_ context.Context, session storage.SessionRecord) error {
	f.records[session.ID] = session
	return nil
}

// notFoundIDs, when set, makes DeleteSession report storage.ErrNotFound
// for that ID instead of actually deleting anything from records — this
// models a row that is already gone by the time RevokeSessionsForUserExcept
// asks for it (e.g. a concurrent Logout, or the DeleteUser flow where the
// user row's ON DELETE CASCADE already removed every session row before
// RevokeSessionsForUser gets a chance to).
//
// Deleting an ID that was never PutSession'd also reports storage.ErrNotFound
// (mirroring the real sqlite/postgres stores, which check RowsAffected == 0
// on `DELETE ... WHERE id = ?`) — this is the ordinary "browser presents a
// stale/bogus cookie" case at login, not merely an edge case, so the fake
// must not silently no-op it.
func (f *selectiveFailSessionStore) DeleteSession(_ context.Context, id string) error {
	if f.failIDs[id] {
		return errors.New("injected store failure")
	}
	if f.notFoundIDs[id] {
		return storage.ErrNotFound
	}
	if _, ok := f.records[id]; !ok {
		return storage.ErrNotFound
	}
	delete(f.records, id)
	return nil
}

func (f *selectiveFailSessionStore) ListSessions(_ context.Context) ([]storage.SessionRecord, error) {
	out := make([]storage.SessionRecord, 0, len(f.records))
	for _, rec := range f.records {
		out = append(out, rec)
	}
	return out, nil
}

func (f *selectiveFailSessionStore) DeleteExpiredSessions(_ context.Context, _ time.Time) error {
	return nil
}

func (f *selectiveFailSessionStore) TouchSession(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (f *selectiveFailSessionStore) hasRecord(id string) bool {
	_, ok := f.records[id]
	return ok
}

// TestRevokeKeepsSessionAliveWhenStoreDeleteFails models the actual threat
// PVX-002 exists for: a stolen session, an owner rotating credentials, and a
// store that refuses one of the deletes. The failed session must survive in
// BOTH the in-memory map (so GetSession still resolves it — proving memory
// and store did not diverge) and the persistent store (so a restart's
// ListSessions-based rehydration does not silently lose the only remaining
// record of it and make a retry a no-op). The other, successfully deleted,
// session must be gone from both places, and the returned count must reflect
// only genuine deletions.
func TestRevokeKeepsSessionAliveWhenStoreDeleteFails(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	target, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "alice",
		Password: "Alice1password",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	sessionA, err := service.Authenticate(context.Background(), LoginInput{
		Username: "alice",
		Password: "Alice1password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(session A) error = %v", err)
	}
	sessionB, err := service.Authenticate(context.Background(), LoginInput{
		Username: "alice",
		Password: "Alice1password",
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(session B) error = %v", err)
	}

	// The store will refuse to delete session A; session B deletes cleanly.
	store.failIDs[sessionA.ID] = true

	n, err := service.RevokeSessionsForUserExcept(context.Background(), target.ID, "")
	if err == nil {
		t.Fatal("RevokeSessionsForUserExcept() error = nil, want non-nil (store delete failed)")
	}
	if n != 1 {
		t.Fatalf("RevokeSessionsForUserExcept() count = %d, want 1 (only session B deleted)", n)
	}

	// Session A: the failed delete must leave it resolvable via GetSession
	// (in-memory map untouched) AND still present in the store (so restart
	// rehydration does not lose it either).
	if _, err := service.GetSession(sessionA.ID); err != nil {
		t.Fatalf("GetSession(sessionA) after failed store delete = %v, want still valid", err)
	}
	if !store.hasRecord(sessionA.ID) {
		t.Fatal("sessionA missing from store after failed delete — a restart's rehydration would lose it and a retry would be a no-op")
	}

	// Session B: successfully revoked from both memory and store.
	if _, err := service.GetSession(sessionB.ID); err == nil {
		t.Fatal("GetSession(sessionB) after successful revoke = nil error, want revoked")
	}
	if store.hasRecord(sessionB.ID) {
		t.Fatal("sessionB still present in store after successful revoke, want deleted")
	}
}

// TestRevokeAllSuccessReturnsCountAndNilError is the happy path: every
// store delete succeeds, both sessions are gone from memory and store, and
// the error return is nil.
func TestRevokeAllSuccessReturnsCountAndNilError(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 30, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	target, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "bob",
		Password: "Bob1password1",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	sessionA, err := service.Authenticate(context.Background(), LoginInput{
		Username: "bob",
		Password: "Bob1password1",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(session A) error = %v", err)
	}
	sessionB, err := service.Authenticate(context.Background(), LoginInput{
		Username: "bob",
		Password: "Bob1password1",
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(session B) error = %v", err)
	}

	n, err := service.RevokeSessionsForUserExcept(context.Background(), target.ID, "")
	if err != nil {
		t.Fatalf("RevokeSessionsForUserExcept() error = %v, want nil", err)
	}
	if n != 2 {
		t.Fatalf("RevokeSessionsForUserExcept() count = %d, want 2", n)
	}

	if _, err := service.GetSession(sessionA.ID); err == nil {
		t.Fatal("GetSession(sessionA) after revoke = nil error, want revoked")
	}
	if _, err := service.GetSession(sessionB.ID); err == nil {
		t.Fatal("GetSession(sessionB) after revoke = nil error, want revoked")
	}
	if store.hasRecord(sessionA.ID) || store.hasRecord(sessionB.ID) {
		t.Fatal("sessions still present in store after full success, want both deleted")
	}
}

// TestRevokeExceptSessionIDIsNeverAttemptedAgainstStore verifies the
// preserved session is not even offered to the store for deletion — it
// should never appear in candidates at all, so a store that fails
// everything cannot accidentally take it down.
func TestRevokeExceptSessionIDIsNeverAttemptedAgainstStore(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	target, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "carol",
		Password: "Carol1password",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	kept, err := service.Authenticate(context.Background(), LoginInput{
		Username: "carol",
		Password: "Carol1password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(kept) error = %v", err)
	}

	n, err := service.RevokeSessionsForUserExcept(context.Background(), target.ID, kept.ID)
	if err != nil {
		t.Fatalf("RevokeSessionsForUserExcept() error = %v, want nil", err)
	}
	if n != 0 {
		t.Fatalf("RevokeSessionsForUserExcept() count = %d, want 0 (only session excepted)", n)
	}
	if _, err := service.GetSession(kept.ID); err != nil {
		t.Fatalf("GetSession(kept) after revoke-except = %v, want preserved", err)
	}
	if !store.hasRecord(kept.ID) {
		t.Fatal("kept session missing from store, want preserved")
	}
}

// TestRevokeTreatsStoreNotFoundAsAlreadyRevoked is a regression test for a
// real bug this plan's own fail-closed change surfaced: the sessions table
// has "ON DELETE CASCADE" on user_id, so DeleteUser's user-row delete already
// removes every session row before RevokeSessionsForUser gets a chance to.
// The subsequent DeleteSession call legitimately returns storage.ErrNotFound
// — the row IS gone, which is exactly the invariant this function exists to
// guarantee — so it must be treated as a successful revocation (counted,
// dropped from memory, no error), not as a persistence failure. Getting this
// wrong made DeleteUser fail closed on every call (500 instead of 204),
// which is a functional regression, not the security fix.
func TestRevokeTreatsStoreNotFoundAsAlreadyRevoked(t *testing.T) {
	now := time.Date(2026, time.July, 17, 10, 30, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	target, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "dave",
		Password: "Dave1password1",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(context.Background(), LoginInput{
		Username: "dave",
		Password: "Dave1password1",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	// Simulate the row already having been removed (e.g. by an FK cascade
	// on a just-deleted user row) by the time the store delete runs, without
	// actually touching store.records — DeleteSession must still report
	// ErrNotFound rather than silently succeeding, so this exercises the
	// real code path.
	store.notFoundIDs[session.ID] = true

	n, err := service.RevokeSessionsForUserExcept(context.Background(), target.ID, "")
	if err != nil {
		t.Fatalf("RevokeSessionsForUserExcept() error = %v, want nil (ErrNotFound is not a failure)", err)
	}
	if n != 1 {
		t.Fatalf("RevokeSessionsForUserExcept() count = %d, want 1 (already-gone row still counts as revoked)", n)
	}
	if _, err := service.GetSession(session.ID); err == nil {
		t.Fatal("GetSession(session) after ErrNotFound-revoke = nil error, want revoked from memory too")
	}
}

// TestLogoutKeepsSessionAliveWhenStoreDeleteFails is Logout's analogue of
// TestRevokeKeepsSessionAliveWhenStoreDeleteFails: a failed store delete
// during logout must leave the session resolvable via GetSession (memory)
// AND still present in the store (ListSessions) — the previous
// unconditional-memory-drop behavior would have made GetSession report
// "revoked" while the row silently survived in the store, resurrecting on
// the next restart. Logout must also return the store's error rather than
// swallowing it into a log-only warning.
func TestLogoutKeepsSessionAliveWhenStoreDeleteFails(t *testing.T) {
	now := time.Date(2026, time.July, 17, 15, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	if _, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "erin",
		Password: "Erin1password1",
		Role:     RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(context.Background(), LoginInput{
		Username: "erin",
		Password: "Erin1password1",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	store.failIDs[session.ID] = true

	if err := service.Logout(context.Background(), session.ID); err == nil {
		t.Fatal("Logout() error = nil, want the injected store failure surfaced")
	}

	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession(session) after failed Logout = %v, want still valid (kept in memory)", err)
	}
	if !store.hasRecord(session.ID) {
		t.Fatal("session missing from store after failed Logout — a restart's rehydration would lose it and a retry would be a no-op")
	}
}

// TestLogoutSucceedsAndRevokesOnHealthyStore is Logout's happy path,
// confirming the D5 lock-discipline rewrite (RUnlock, store I/O, re-Lock to
// drop) still produces ordinary successful logout behavior.
func TestLogoutSucceedsAndRevokesOnHealthyStore(t *testing.T) {
	now := time.Date(2026, time.July, 17, 15, 5, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	if _, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "frank",
		Password: "Frank1password1",
		Role:     RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(context.Background(), LoginInput{
		Username: "frank",
		Password: "Frank1password1",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if err := service.Logout(context.Background(), session.ID); err != nil {
		t.Fatalf("Logout() error = %v, want nil", err)
	}
	if _, err := service.GetSession(session.ID); err == nil {
		t.Fatal("GetSession(session) after successful Logout = nil error, want revoked")
	}
	if store.hasRecord(session.ID) {
		t.Fatal("session still present in store after successful Logout, want deleted")
	}
}

// TestLogoutFailureLeavesSessionForSubsequentRevoke is the composed
// regression this whole follow-up exists for: Logout's store delete fails,
// and a LATER password change on that same user must not see zero
// candidates and silently report success while the session row still
// exists in the store. Before the D1 fix, Logout dropped the session from
// memory unconditionally, so RevokeSessionsForUserExcept (called from
// UpdateUser) would find nothing to revoke, report (0, nil), and UpdateUser
// would happily persist the new password — while the orphaned row in the
// store kept authenticating the old cookie until natural absolute expiry.
// This test asserts the real, end-to-end outcome (UpdateUser must fail
// closed), not just the intermediate fact that Logout returned an error.
func TestLogoutFailureLeavesSessionForSubsequentRevoke(t *testing.T) {
	now := time.Date(2026, time.July, 17, 15, 10, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	target, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "grace",
		Password: "Grace1password1",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(context.Background(), LoginInput{
		Username: "grace",
		Password: "Grace1password1",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	// The store refuses the delete both now (Logout) and later (the
	// password-change revoke pass) — modeling a store outage that outlives
	// a single request, so the composed scenario is genuinely exercised
	// rather than incidentally fixed by a lucky retry.
	store.failIDs[session.ID] = true

	if err := service.Logout(context.Background(), session.ID); err == nil {
		t.Fatal("Logout() error = nil, want store-delete failure surfaced")
	}

	// Sanity precondition: the row must have survived the failed logout —
	// otherwise this test would not be exercising the composed scenario at
	// all (it would degrade to "zero candidates, nothing to check").
	if !store.hasRecord(session.ID) {
		t.Fatal("session missing from store after failed Logout — composed scenario cannot be exercised")
	}

	_, err = service.UpdateUser(context.Background(), UpdateUserInput{
		UserID:      target.ID,
		Username:    target.Username,
		Role:        target.Role,
		NewPassword: "NewGrace1password",
	}, now.Add(5*time.Minute))
	if err == nil {
		t.Fatal("UpdateUser() error = nil, want fail-closed: the still-surviving session row must block a silent success")
	}

	// The real outcome, not an intermediate: the credential must be
	// unchanged (old password still verifies), and the never-actually-
	// revoked session must still resolve — nothing was silently lost.
	stored, err := service.GetUserByID(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if err := service.VerifyPassword(stored.PasswordHash, "Grace1password1"); err != nil {
		t.Fatalf("VerifyPassword(original password) after failed UpdateUser = %v, want still valid", err)
	}
	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession(session) after failed UpdateUser = %v, want still valid", err)
	}
	if !store.hasRecord(session.ID) {
		t.Fatal("session missing from store after the composed failure sequence — it must still survive, never having been genuinely revoked")
	}
}

// TestAuthenticateRejectsLoginWhenPriorSessionStoreDeleteFails covers the
// prior-session-purge fail-closed fix in persistAuthenticatedSession: when
// the browser carries a prior session cookie (P) into a login and the store
// refuses to delete P's row, the login must be REJECTED — not proceed to
// issue a fresh session while P's row survives in the store. Before this
// fix, Authenticate dropped P from the in-memory map BEFORE the store
// delete was attempted, so a store failure here left P alive in the store
// but forgotten in memory (invisible to any later revoke pass).
func TestAuthenticateRejectsLoginWhenPriorSessionStoreDeleteFails(t *testing.T) {
	now := time.Date(2026, time.July, 17, 17, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	if _, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "heidi",
		Password: "Heidi1password1",
		Role:     RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// P: the victim's real (or attacker-planted/stolen) session.
	priorSession, err := service.Authenticate(context.Background(), LoginInput{
		Username: "heidi",
		Password: "Heidi1password1",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(prior) error = %v", err)
	}

	// The store now refuses to delete P.
	store.failIDs[priorSession.ID] = true

	// The browser carries P's cookie into a second login attempt.
	_, err = service.Authenticate(context.Background(), LoginInput{
		Username:       "heidi",
		Password:       "Heidi1password1",
		PriorSessionID: priorSession.Cookie,
	}, now.Add(2*time.Minute))
	if err == nil {
		t.Fatal("Authenticate() error = nil, want rejected login when the prior-session store purge fails")
	}
	if !errors.Is(err, ErrSessionStoreUnavailable) {
		t.Fatalf("Authenticate() error = %v, want wrapping ErrSessionStoreUnavailable", err)
	}

	// P must be untouched by the rejected attempt: still valid in memory
	// and still present in the store.
	if _, err := service.GetSession(priorSession.ID); err != nil {
		t.Fatalf("GetSession(P) after rejected login = %v, want still valid", err)
	}
	if !store.hasRecord(priorSession.ID) {
		t.Fatal("P missing from store after rejected login")
	}

	// No new session was issued for the rejected attempt.
	service.mu.RLock()
	count := len(service.sessions)
	service.mu.RUnlock()
	if count != 1 {
		t.Fatalf("in-memory session count = %d, want 1 (only P; no new session issued on rejection)", count)
	}
}

// TestPriorSessionPurgeFailureThenPasswordChangeDoesNotResurrect is the
// composed regression: after a login attempt fails to purge a prior
// session P from the store, a LATER password change on that user must not
// see zero candidates and silently report success while P still survives.
// This is the real end-to-end threat traced by the coordinator: a stolen
// cookie P, a re-login attempt that fails to purge it from the store, and
// a subsequent password rotation that the victim believes killed every
// other session. Asserts the real outcome (UpdateUser must fail closed),
// not just that Authenticate returned an error.
func TestPriorSessionPurgeFailureThenPasswordChangeDoesNotResurrect(t *testing.T) {
	now := time.Date(2026, time.July, 17, 17, 10, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	target, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "ivan",
		Password: "Ivan1password1",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	priorSession, err := service.Authenticate(context.Background(), LoginInput{
		Username: "ivan",
		Password: "Ivan1password1",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate(prior) error = %v", err)
	}

	// The store refuses to delete P both during the re-login attempt and
	// later during the password-change revoke pass, modeling an outage
	// that outlives a single request.
	store.failIDs[priorSession.ID] = true

	if _, err := service.Authenticate(context.Background(), LoginInput{
		Username:       "ivan",
		Password:       "Ivan1password1",
		PriorSessionID: priorSession.Cookie,
	}, now.Add(2*time.Minute)); err == nil {
		t.Fatal("Authenticate() error = nil, want rejected login when the prior-session store purge fails")
	}

	// Sanity precondition: P must have survived the failed login attempt.
	if !store.hasRecord(priorSession.ID) {
		t.Fatal("P missing from store after rejected login — composed scenario cannot be exercised")
	}

	// The victim now rotates their password, believing every other
	// session (including the possibly-attacker-held P) is dead. Because
	// the rejected login never touched memory, P is still a genuine
	// candidate for revocation here — and the store still refuses to
	// delete it — so this must fail closed, not report success.
	_, err = service.UpdateUser(context.Background(), UpdateUserInput{
		UserID:      target.ID,
		Username:    target.Username,
		Role:        target.Role,
		NewPassword: "NewIvan1password",
	}, now.Add(5*time.Minute))
	if err == nil {
		t.Fatal("UpdateUser() error = nil, want fail-closed: P must still block a silent success")
	}

	// The real outcome: credential unchanged, P still resolves, still in
	// the store — nothing was silently lost or resurrected.
	stored, err := service.GetUserByID(context.Background(), target.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if err := service.VerifyPassword(stored.PasswordHash, "Ivan1password1"); err != nil {
		t.Fatalf("VerifyPassword(original password) after failed UpdateUser = %v, want still valid", err)
	}
	if _, err := service.GetSession(priorSession.ID); err != nil {
		t.Fatalf("GetSession(P) after failed UpdateUser = %v, want still valid", err)
	}
	if !store.hasRecord(priorSession.ID) {
		t.Fatal("P missing from store after the composed failure sequence — it must still survive, never having been genuinely revoked")
	}
}

// TestAuthenticateSucceedsWhenPriorSessionAlreadyGoneFromStore is the
// CRITICAL normal-case coverage the coordinator flagged: a browser
// routinely presents a stale, expired, or bogus cookie at login, whose row
// was never in the store (or was already reaped). storage.ErrNotFound from
// the prior-session purge must be treated as success, not a login-blocking
// failure — otherwise every login carrying a stale cookie would break.
func TestAuthenticateSucceedsWhenPriorSessionAlreadyGoneFromStore(t *testing.T) {
	now := time.Date(2026, time.July, 17, 17, 20, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })

	store := newSelectiveFailSessionStore()
	service.SetSessionStore(store)

	if _, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "judy",
		Password: "Judy1password1",
		Role:     RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// A cookie the store has never heard of — never PutSession'd, so
	// DeleteSession reports storage.ErrNotFound (see the fake's doc
	// comment: this mirrors the real stores' RowsAffected == 0 behavior).
	session, err := service.Authenticate(context.Background(), LoginInput{
		Username:       "judy",
		Password:       "Judy1password1",
		PriorSessionID: "stale-cookie-never-issued-by-this-service",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v, want success — a stale prior-session cookie must not block login", err)
	}
	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession(new session) error = %v, want valid", err)
	}
}
