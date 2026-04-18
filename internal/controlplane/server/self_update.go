package server

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// The real implementations live in controlplane/updates (task
// P3-ARCH-01d). This file keeps the old package-level function names
// around as thin delegators so that http_updates.go keeps compiling
// without call-site churn during this extraction pass.

// DownloadArchive fetches a .tar.gz archive from url. See
// updates.DownloadArchive for details.
func DownloadArchive(ctx context.Context, url, token string) (string, error) {
	return updates.DownloadArchive(ctx, url, token)
}

// ExtractBinaryFromArchive extracts the first file from a .tar.gz
// archive into a temporary executable. See
// updates.ExtractBinaryFromArchive for details.
func ExtractBinaryFromArchive(archivePath string) (string, error) {
	return updates.ExtractBinaryFromArchive(archivePath)
}

// DownloadChecksum fetches a .sha256 checksum file and returns the hex
// digest. See updates.DownloadChecksum for details.
func DownloadChecksum(ctx context.Context, url, token string) (string, error) {
	return updates.DownloadChecksum(ctx, url, token)
}

// DownloadSignature fetches a detached signature file and returns its
// bytes. See updates.DownloadSignature for details.
func DownloadSignature(ctx context.Context, url, token string) ([]byte, error) {
	return updates.DownloadSignature(ctx, url, token)
}

// VerifyChecksum computes the SHA256 of the file at path and compares
// it to the expected hex digest. See updates.VerifyChecksum for details.
func VerifyChecksum(path, expected string) error {
	return updates.VerifyChecksum(path, expected)
}

// AtomicReplaceBinary replaces the binary at currentPath with the one
// at newPath. See updates.AtomicReplaceBinary for details.
func AtomicReplaceBinary(currentPath, newPath string) error {
	return updates.AtomicReplaceBinary(currentPath, newPath)
}
