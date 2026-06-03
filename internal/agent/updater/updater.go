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
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

// defaultMaxChecksum bounds the .sha256 sidecar fetch. The file is a single
// hex digest (64 chars) plus optional filename; 1 KiB is generous.
const defaultMaxChecksum = 1 << 10

// parseChecksumSidecar extracts the hex digest from a `.sha256` sidecar.
// CI writes just the digest, but `sha256sum` output ("<hex>  <file>") is
// also tolerated by taking the first whitespace-delimited field.
func parseChecksumSidecar(b []byte) string {
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// Payload is the JSON payload of an agent.self-update job. The panel sends
// only the release directory and target version; the agent resolves the
// per-architecture asset names itself, so the panel can never pick the
// wrong arch.
type Payload struct {
	Version        string `json:"version"`
	ReleaseBaseURL string `json:"release_base_url"`
	// AllowDowngrade lets an operator install an older binary on purpose
	// (e.g. emergency rollback). Without it the agent refuses any version
	// below the running one — protects against a compromised panel pinning
	// agents to a vulnerable past release.
	AllowDowngrade bool `json:"allow_downgrade,omitempty"`
}

// Execute performs the self-update: download, verify checksum (mandatory),
// extract, replace, restart.
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
	// Downgrade gate (fail-closed). Defeats a hostile panel pinning agents
	// back to a vulnerable release, so the escape hatches are explicit.
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

	// The agent substitutes its OWN architecture — the panel never chooses
	// it, so it cannot send the wrong-arch binary.
	base := strings.TrimRight(strings.TrimSpace(payload.ReleaseBaseURL), "/")
	if base == "" {
		return fmt.Errorf("no release base URL provided")
	}
	archiveName := fmt.Sprintf("panvex-agent-linux-%s.tar.gz", runtime.GOARCH)
	archiveURL := base + "/" + archiveName
	checksumURL := archiveURL + ".sha256"

	logger.Info("agent self-update: downloading", "version", payload.Version, "url", archiveURL)

	archivePath, err := downloadToTemp(ctx, archiveURL, cfg)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	checksumBytes, err := downloadBytes(ctx, checksumURL, defaultMaxChecksum, cfg)
	if err != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("download checksum: %w", err)
	}
	expectedChecksum := parseChecksumSidecar(checksumBytes)
	if expectedChecksum == "" {
		_ = os.Remove(archivePath)
		return fmt.Errorf("verify: checksum sidecar is empty or malformed")
	}
	if err := verifyChecksum(archivePath, expectedChecksum); err != nil {
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

	// Attempt systemd restart. On success systemd tears this process down.
	// On failure we must exit NON-ZERO so the unit's `Restart=on-failure`
	// relaunches the already-replaced binary — exit code 0 would not be
	// restarted (and 78 is RestartPreventExitStatus, so it must not be 78).
	if err := exec.CommandContext(ctx, "systemctl", "restart", "panvex-agent").Start(); err != nil {
		logger.Warn("systemctl restart failed, exiting non-zero for on-failure restart", "error", err)
		os.Exit(1)
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
