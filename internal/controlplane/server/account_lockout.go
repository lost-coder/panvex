package server

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/sessions"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// The lockout tracker itself lives in controlplane/sessions (task
// P3-ARCH-01c). This file keeps the old lowercase names around as type
// aliases + package-local constants so that http_auth.go, metrics.go,
// and http_auth_test.go keep compiling without a mass rename.

const (
	accountLockoutMaxAttempts = sessions.LockoutMaxAttempts
	accountLockoutDuration    = sessions.LockoutDuration
)

type accountLockoutTracker = sessions.LockoutTracker

func newAccountLockoutTracker() *accountLockoutTracker {
	return sessions.NewLockoutTracker()
}

// S-6: separate, stricter counter for TOTP failures so that an attacker
// who already has the password cannot brute-force the 6-digit code at
// the password lockout's 5-attempts-per-15-min budget. The new tracker
// trips at 3 attempts per 5 min and is in-memory only — survival across
// a control-plane restart adds no value because the user must produce a
// fresh code on retry anyway.
const (
	totpLockoutMaxAttempts = sessions.TOTPLockoutMaxAttempts
	totpLockoutDuration    = sessions.TOTPLockoutDuration
)

type totpLockoutTracker = sessions.TOTPLockoutTracker

func newTOTPLockoutTracker() *totpLockoutTracker {
	return sessions.NewTOTPLockoutTracker()
}

// S-medium (Task 6): a third lockout tracker keyed by source IP closes
// the targeted-DoS gap left by the username-keyed counter. An attacker
// who enumerates usernames and triggers 5 failures against each can
// otherwise lock every account in turn; counting failures per source
// IP raises the cost of that attack without affecting legitimate
// fat-fingering in normal usage.
const (
	ipLockoutMaxFailures = sessions.IPLockoutMaxFailures
	ipLockoutWindow      = sessions.IPLockoutWindow
	ipLockoutDuration    = sessions.IPLockoutDuration
)

type ipLockoutTracker = sessions.IPLockoutTracker

func newIPLockoutTracker() *ipLockoutTracker {
	return sessions.NewIPLockoutTracker()
}

// lockoutStoreAdapter bridges sessions.LockoutStore (defined locally in
// the sessions package to keep it free of storage imports) and
// storage.LoginLockoutStore (the real persistence interface). The
// adapter is the canonical translation between the two record types
// and lives here so the wiring seam is explicit.
type lockoutStoreAdapter struct {
	store storage.LoginLockoutStore
}

func newLockoutStoreAdapter(store storage.LoginLockoutStore) *lockoutStoreAdapter {
	return &lockoutStoreAdapter{store: store}
}

func (a *lockoutStoreAdapter) UpsertLoginLockout(ctx context.Context, record sessions.LockoutRecord) error {
	return a.store.UpsertLoginLockout(ctx, storage.LoginLockoutRecord{
		Username:  record.Username,
		Failures:  record.Failures,
		LockedAt:  record.LockedAt,
		UpdatedAt: record.UpdatedAt,
	})
}

func (a *lockoutStoreAdapter) GetLoginLockout(ctx context.Context, username string) (sessions.LockoutRecord, error) {
	record, err := a.store.GetLoginLockout(ctx, username)
	if err != nil {
		return sessions.LockoutRecord{}, err
	}
	return sessions.LockoutRecord{
		Username:  record.Username,
		Failures:  record.Failures,
		LockedAt:  record.LockedAt,
		UpdatedAt: record.UpdatedAt,
	}, nil
}

func (a *lockoutStoreAdapter) DeleteLoginLockout(ctx context.Context, username string) error {
	return a.store.DeleteLoginLockout(ctx, username)
}

func (a *lockoutStoreAdapter) ListLoginLockouts(ctx context.Context) ([]sessions.LockoutRecord, error) {
	records, err := a.store.ListLoginLockouts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sessions.LockoutRecord, 0, len(records))
	for _, r := range records {
		out = append(out, sessions.LockoutRecord{
			Username:  r.Username,
			Failures:  r.Failures,
			LockedAt:  r.LockedAt,
			UpdatedAt: r.UpdatedAt,
		})
	}
	return out, nil
}

func (a *lockoutStoreAdapter) DeleteExpiredLoginLockouts(ctx context.Context, before time.Time) (int64, error) {
	return a.store.DeleteExpiredLoginLockouts(ctx, before)
}
