// internal/controlplane/auth/repository.go
//
// Package-local persistence contracts for the auth Service (A6). The
// Service historically declared its store fields in terms of the
// storage-package sub-interfaces (storage.UserStore, storage.SessionStore,
// storage.ConsumedTotpStore). Those are already narrow, but they live in
// the storage package, so the auth package's declared dependency surface
// was defined elsewhere and was wider than what auth actually calls (e.g.
// storage.SessionStore.GetSession is never invoked — auth.GetSession reads
// the in-memory map).
//
// Following the same pattern as jobs.Store (D1) and fleet.Repository (D2),
// the interfaces below list EXACTLY the methods auth.Service invokes,
// co-located with the consumer. The concrete *sqlite.Store / *postgres.Store
// — via storage.UserStore / storage.SessionStore / storage.ConsumedTotpStore
// — satisfy these subsets, so wiring at the call site (server/lifecycle.go)
// is unchanged. Test doubles only need to cover these methods.
//
// The fields stay split across three injectors (NewServiceWithStore,
// SetSessionStore, SetConsumedTotpStore) and remain independently
// nil-checkable, exactly as before — this is type-narrowing only, no
// behaviour change.
package auth

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// UserStore is the subset of storage.UserStore the auth Service uses for
// local-account persistence. auth calls every method of storage.UserStore,
// so this is a verbatim restatement co-located with the consumer.
type UserStore interface {
	PutUser(ctx context.Context, user storage.UserRecord) error
	DeleteUser(ctx context.Context, userID string) error
	GetUserByID(ctx context.Context, userID string) (storage.UserRecord, error)
	GetUserByUsername(ctx context.Context, username string) (storage.UserRecord, error)
	ListUsers(ctx context.Context) ([]storage.UserRecord, error)
}

// SessionStore is the subset of storage.SessionStore the auth Service uses.
// It omits GetSession: auth.GetSession serves reads from the in-memory
// session map, never from the persistent store, so the store's GetSession
// is dead surface from auth's perspective.
type SessionStore interface {
	PutSession(ctx context.Context, session storage.SessionRecord) error
	DeleteSession(ctx context.Context, sessionID string) error
	ListSessions(ctx context.Context) ([]storage.SessionRecord, error)
	DeleteExpiredSessions(ctx context.Context, before time.Time) error
	// TouchSession persists a refreshed LastSeenAt so the sliding idle
	// timeout survives a control-plane restart (Q2.U-S-12).
	TouchSession(ctx context.Context, sessionID string, lastSeenAt time.Time) error
}
