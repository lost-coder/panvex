package server

import (
	"net/http"

	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// The real implementations live in controlplane/updates (task
// P3-ARCH-01d). This file keeps the old lowercase package-private
// helpers around as thin delegators so that http_updates.go and
// update_checker.go call sites don't need a mass rename in this
// extraction pass.

func validateGitHubRepo(s string) error { return updates.ValidateGitHubRepo(s) }

func checkDownloadURL(raw string) error { return updates.CheckDownloadURL(raw) }

func restrictedRedirectPolicy() func(req *http.Request, via []*http.Request) error {
	return updates.RestrictedRedirectPolicy()
}

func secureDownloadClient() *http.Client { return updates.SecureDownloadClient() }
