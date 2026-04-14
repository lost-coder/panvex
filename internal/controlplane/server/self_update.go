package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// DownloadBinary fetches a binary from url into a temporary file and returns
// its path. The caller is responsible for removing the file when done.
func DownloadBinary(ctx context.Context, url, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download binary: unexpected status %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "panvex-update-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("write binary: %w", err)
	}
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("chmod binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("close binary: %w", err)
	}

	return tmp.Name(), nil
}

// DownloadChecksum fetches a .sha256 checksum file and returns the hex digest.
// The file is expected to contain the checksum as the first whitespace-delimited
// field on the first line.
func DownloadChecksum(ctx context.Context, url, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download checksum: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download checksum: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("read checksum: %w", err)
	}

	line := strings.TrimSpace(string(body))
	if line == "" {
		return "", fmt.Errorf("checksum file is empty")
	}

	// The first field is the hex-encoded SHA256 hash.
	fields := strings.Fields(line)
	return fields[0], nil
}

// VerifyChecksum computes the SHA256 of the file at path and compares it to
// the expected hex digest. Returns nil on match, an error on mismatch.
func VerifyChecksum(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file for checksum: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash file: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch: got %s, want %s", actual, expected)
	}
	return nil
}

// AtomicReplaceBinary replaces the binary at currentPath with the one at
// newPath. The original binary is preserved as currentPath + ".bak".
func AtomicReplaceBinary(currentPath, newPath string) error {
	backupPath := currentPath + ".bak"

	// Remove any stale backup from a previous update.
	_ = os.Remove(backupPath)

	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}

	if err := os.Rename(newPath, currentPath); err != nil {
		// Attempt to restore the backup on failure.
		_ = os.Rename(backupPath, currentPath)
		return fmt.Errorf("replace binary: %w", err)
	}

	return nil
}
