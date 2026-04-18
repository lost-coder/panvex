// Package updates holds the control-plane's self-update subsystem:
// release discovery, artifact download, signature/checksum verification,
// and atomic binary replacement.
//
// This is task P3-ARCH-01d of the god-package split (remediation plan v4).
// The package currently exports:
//
//   - GitHubRelease, GitHubReleaseAsset DTOs and the pure tag/version
//     helpers (ParseReleaseTag, CompareVersions).
//   - FetchLatestVersions / ResolveAssetURLs for querying the GitHub
//     Releases API.
//   - DownloadArchive, DownloadChecksum, DownloadSignature plus the
//     ExtractBinaryFromArchive / VerifyChecksum / AtomicReplaceBinary
//     helpers used by the apply path.
//   - ValidateGitHubRepo, CheckDownloadURL and SecureDownloadClient —
//     the hardening layer that keeps release fetches scoped to GitHub
//     hosts regardless of repo-setting misconfiguration.
//
// The `update_checker` background worker and the HTTP handlers in
// controlplane/server continue to own orchestration (settings lookup,
// audit, job enqueue). They now delegate all I/O + verification to the
// helpers in this package.
package updates
