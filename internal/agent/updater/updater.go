package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/lost-coder/panvex/internal/security"
)

// Payload is the JSON payload of an agent.self-update job.
type Payload struct {
	Version          string `json:"version"`
	DownloadURL      string `json:"download_url"`
	ChecksumSHA256   string `json:"checksum_sha256"`
	SignatureURL     string `json:"signature_url"`
	DownloadViaPanel bool   `json:"download_via_panel"`
	PanelProxyURL    string `json:"panel_proxy_url,omitempty"`
}

// Execute performs the self-update: download, verify signature (required),
// verify checksum (defence-in-depth), extract, replace, restart.
func Execute(ctx context.Context, payload Payload, logger *slog.Logger) error {
	url := payload.DownloadURL
	if payload.DownloadViaPanel && payload.PanelProxyURL != "" {
		url = payload.PanelProxyURL
	}
	if url == "" {
		return fmt.Errorf("no download URL provided")
	}
	if payload.SignatureURL == "" {
		return fmt.Errorf("no signature URL provided; refusing to install unsigned update")
	}

	logger.Info("agent self-update: downloading", "version", payload.Version, "url", url)

	archivePath, err := downloadToTemp(ctx, url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	sigBytes, err := downloadBytes(ctx, payload.SignatureURL, 4096)
	if err != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("download signature: %w", err)
	}
	archiveBytes, err := os.ReadFile(archivePath) //nolint:gosec // path created by downloadToTemp
	if err != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("read archive for signature check: %w", err)
	}
	if err := security.VerifyArtifactBytes(archiveBytes, sigBytes); err != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("verify signature: %w", err)
	}
	logger.Info("agent self-update: signature verified", "version", payload.Version)

	if err := verifyChecksum(archivePath, payload.ChecksumSHA256); err != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("verify: %w", err)
	}

	binaryPath, err := extractBinaryFromArchive(archivePath)
	_ = os.Remove(archivePath)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	currentPath, err := os.Executable()
	if err != nil {
		_ = os.Remove(binaryPath)
		return fmt.Errorf("resolve executable: %w", err)
	}

	if err := replaceSelf(currentPath, binaryPath); err != nil {
		_ = os.Remove(binaryPath)
		return fmt.Errorf("replace: %w", err)
	}

	// replaceBinary renames tmpPath into place, so no cleanup needed.
	logger.Info("agent self-update: binary replaced, restarting", "version", payload.Version)

	// Attempt systemd restart. If it fails, exit to let systemd auto-restart.
	if err := exec.Command("systemctl", "restart", "panvex-agent").Start(); err != nil {
		logger.Warn("systemctl restart failed, exiting for auto-restart", "error", err)
	}
	os.Exit(0)
	return nil // unreachable
}

// downloadBytes fetches url and returns its body, bounded to maxBytes. Used
// for small companion files (signature, checksum) where streaming to disk is
// unnecessary.
func downloadBytes(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response body")
	}
	return body, nil
}

func downloadToTemp(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "panvex-agent-update-*")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0755); err != nil { //nolint:gosec // executable binary requires 0755
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func verifyChecksum(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch: got %s, want %s", actual, expected)
	}
	return nil
}

func extractBinaryFromArchive(archivePath string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	if _, err := tr.Next(); err != nil {
		return "", fmt.Errorf("read tar entry: %w", err)
	}

	tmp, err := os.CreateTemp("", "panvex-agent-binary-*")
	if err != nil {
		return "", fmt.Errorf("create temp binary: %w", err)
	}

	const maxBinarySize = 256 << 20 // 256 MB
	if _, err := io.Copy(tmp, io.LimitReader(tr, maxBinarySize)); err != nil {
		tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("extract binary: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0755); err != nil { //nolint:gosec // executable binary requires 0755
		tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("chmod binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("close binary: %w", err)
	}
	return tmp.Name(), nil
}

func replaceSelf(currentPath, newPath string) error {
	backupPath := currentPath + ".bak"
	_ = os.Remove(backupPath)
	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := os.Rename(newPath, currentPath); err != nil {
		_ = os.Rename(backupPath, currentPath)
		return fmt.Errorf("replace: %w", err)
	}
	return nil
}
