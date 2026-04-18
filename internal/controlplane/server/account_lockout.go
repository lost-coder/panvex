package server

import (
	"github.com/lost-coder/panvex/internal/controlplane/sessions"
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
