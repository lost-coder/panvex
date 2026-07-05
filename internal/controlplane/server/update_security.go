package server

import (
	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// The real implementations live in controlplane/updates (task
// P3-ARCH-01d). This file keeps the one still-called lowercase helper as
// a thin delegator so http_updates.go call sites don't need a rename.
//
// The restrictedRedirectPolicy / checkDownloadURL / secureDownloadClient
// delegators were retired in P5 together with the dead agent-binary proxy
// endpoint (their only caller). Redirect restriction and download-URL
// validation for the live manual-update paths are wired entirely inside
// controlplane/updates (download.go / release.go via SecureDownloadClient,
// covered by updates/security_test.go).

func validateGitHubRepo(s string) error { return updates.ValidateGitHubRepo(s) }
