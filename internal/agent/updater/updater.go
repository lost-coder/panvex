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
	"os"
	"os/exec"
	"strings"

	"github.com/lost-coder/panvex/internal/security"
	"golang.org/x/mod/semver"
)

// Payload is the JSON payload of an agent.self-update job.
type Payload struct {
	Version          string `json:"version"`
	DownloadURL      string `json:"download_url"`
	ChecksumSHA256   string `json:"checksum_sha256"`
	SignatureURL     string `json:"signature_url"`
	DownloadViaPanel bool   `json:"download_via_panel"`
	PanelProxyURL    string `json:"panel_proxy_url,omitempty"`
	// AllowDowngrade lets an operator install an older binary on
	// purpose (e.g. emergency rollback). Without it the agent refuses
	// any version below the running one — protects against a
	// compromised panel pinning agents to a vulnerable past release.
	AllowDowngrade bool `json:"allow_downgrade,omitempty"`
}

// Execute performs the self-update: download, verify signature (required),
// verify checksum (defence-in-depth), extract, replace, restart.
//
// currentVersion is the running agent's compiled-in version string
// (cmd/agent/main.go's AgentVersion ldflag). It is compared to
// payload.Version so a panel that tries to silently roll an agent
// back to a vulnerable older release is rejected.
func Execute(ctx context.Context, payload Payload, currentVersion string, logger *slog.Logger) error {
	return executeWith(ctx, payload, currentVersion, logger, defaultConfig())
}

// executeWith is the testable form: same logic but the download policy
// (HTTP client, host allowlist, archive cap) comes from cfg instead of
// hard-coded production defaults.
func executeWith(ctx context.Context, payload Payload, currentVersion string, logger *slog.Logger, cfg Config) error {
	url := payload.DownloadURL
	dlCfg := cfg
	// Panel-proxy mode: the URL came from the panel this agent is enrolled
	// with (mTLS-authenticated). Skip the public allowlist — the operator
	// may legitimately host releases on the panel itself — but still
	// require the same scheme/timeout/size policy.
	if payload.DownloadViaPanel && payload.PanelProxyURL != "" {
		url = payload.PanelProxyURL
		dlCfg.AllowedHosts = nil
	}
	if url == "" {
		return fmt.Errorf("no download URL provided")
	}
	if payload.SignatureURL == "" {
		return fmt.Errorf("no signature URL provided; refusing to install unsigned update")
	}

	// Downgrade gate (fail-closed). The whole point of this check is to
	// defeat a hostile panel that pins agents back to a vulnerable
	// release, so the un-pinned escape hatches must be explicit.
	//
	// - The agent's own version must parse as a real semver. A binary
	//   without ldflags ("dev", "", "snapshot") cannot prove its lineage,
	//   so any update must be opted in via AllowDowngrade.
	// - The payload version must also parse as a real semver. A panel
	//   that omits the field is sending an unsigned-in-spirit update;
	//   refuse it for the same reason we refuse missing SignatureURL.
	// - golang.org/x/mod/semver gives proper pre-release/build-metadata
	//   ordering, so "1.4.7-rc1" sorts below "1.4.7", not equal.
	if !payload.AllowDowngrade {
		curr, currOK := canonicalSemver(currentVersion)
		next, nextOK := canonicalSemver(payload.Version)
		switch {
		case !currOK:
			return fmt.Errorf(
				"refusing self-update: running version %q is not a parseable semver (set allow_downgrade=true to override; typically only the panel-issued production binary has a real version)",
				currentVersion,
			)
		case !nextOK:
			return fmt.Errorf(
				"refusing self-update: payload version %q is not a parseable semver (set allow_downgrade=true to override)",
				payload.Version,
			)
		case semver.Compare(next, curr) < 0:
			return fmt.Errorf(
				"refusing downgrade: payload version %q is older than running version %q (set allow_downgrade=true on the job to override)",
				payload.Version, currentVersion,
			)
		}
	}

	logger.Info("agent self-update: downloading", "version", payload.Version, "url", url)

	archivePath, err := downloadToTemp(ctx, url, dlCfg)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	sigBytes, err := downloadBytes(ctx, payload.SignatureURL, defaultMaxSignature, dlCfg)
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
	// The os.Exit(0) below makes the child a systemd-adopted orphan, so the
	// ctx attachment here is mostly cosmetic — Start() is non-blocking and
	// the parent process is gone before any cancellation could fire.
	if err := exec.CommandContext(ctx, "systemctl", "restart", "panvex-agent").Start(); err != nil {
		logger.Warn("systemctl restart failed, exiting for auto-restart", "error", err)
	}
	os.Exit(0)
	return nil // unreachable
}

// canonicalSemver normalises an operator- or panel-supplied version
// string into the leading-"v" form that golang.org/x/mod/semver
// expects, and reports whether the result is a real semver.
//
// Rules:
//   - "" / "dev" / "snapshot" → not parseable (caller treats as
//     "no provable version", fails closed).
//   - "1.4.7" or "v1.4.7" → "v1.4.7", ok.
//   - "1.4.7-rc1+build" → "v1.4.7-rc1+build", ok (semver handles
//     pre-release ordering correctly: 1.4.7-rc1 < 1.4.7).
//   - "alpha" / "main" / "1.x" → not parseable.
func canonicalSemver(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "dev") || strings.EqualFold(v, "snapshot") {
		return "", false
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", false
	}
	return v, true
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
	if err := os.Chmod(tmp.Name(), 0o700); err != nil { //nolint:gosec // G302: 0o700 keeps the binary owner-only but still needs +x to run
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
