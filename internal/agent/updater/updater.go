package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lost-coder/panvex/internal/updatehosts"
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

// Outcome reports what Execute actually did, so the caller (the job
// handler) can decide whether a process restart must be scheduled.
type Outcome int

const (
	// OutcomeNoop: nothing was downloaded or replaced — the agent already
	// runs the requested version. Also the zero value, so error returns
	// never read as "updated".
	OutcomeNoop Outcome = iota
	// OutcomeUpdated: the binary was downloaded, verified and swapped in
	// place. The caller MUST schedule a process restart — and must do so
	// only after the JobResult has been handed off (A3).
	OutcomeUpdated
)

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
// extract, replace. Returns OutcomeUpdated when the binary was swapped in
// place; the caller MUST schedule a process restart after handing off the
// JobResult (A3 — never restart inside the job handler).
//
// currentVersion is the running agent's compiled-in version string
// (cmd/agent/main.go's AgentVersion ldflag). It is compared to
// payload.Version so a panel that tries to silently roll an agent
// back to a vulnerable older release is rejected.
func Execute(ctx context.Context, payload Payload, currentVersion string, logger *slog.Logger) (Outcome, error) {
	return executeWith(ctx, payload, currentVersion, logger, defaultConfig())
}

// executeWith is the testable form: same logic but the download policy
// (HTTP client, host allowlist, archive cap) comes from cfg instead of
// hard-coded production defaults.
func executeWith(ctx context.Context, payload Payload, currentVersion string, logger *slog.Logger, cfg Config) (Outcome, error) {
	// Downgrade gate (fail-closed). Defeats a hostile panel pinning agents
	// back to a vulnerable release, so the escape hatches are explicit.
	if !payload.AllowDowngrade {
		curr, currOK := canonicalSemver(currentVersion)
		next, nextOK := canonicalSemver(payload.Version)
		switch {
		case !currOK:
			return OutcomeNoop, fmt.Errorf(
				"refusing self-update: running version %q is not a parseable semver (set allow_downgrade=true to override; typically only the panel-issued production binary has a real version)",
				currentVersion,
			)
		case !nextOK:
			return OutcomeNoop, fmt.Errorf(
				"refusing self-update: payload version %q is not a parseable semver (set allow_downgrade=true to override)",
				payload.Version,
			)
		case semver.Compare(next, curr) == 0:
			// A3: already at the target version — converge as a successful
			// no-op instead of reinstalling and restarting forever.
			// AllowDowngrade=true skips this branch on purpose: it is the
			// operator's escape hatch for forced reinstall (binary repair).
			logger.Info("agent self-update: already at target version", "version", currentVersion)
			return OutcomeNoop, nil
		case semver.Compare(next, curr) < 0:
			return OutcomeNoop, fmt.Errorf(
				"refusing downgrade: payload version %q is older than running version %q (set allow_downgrade=true on the job to override)",
				payload.Version, currentVersion,
			)
		}
	}

	// The agent substitutes its OWN architecture — the panel never chooses
	// it, so it cannot send the wrong-arch binary.
	base := strings.TrimRight(strings.TrimSpace(payload.ReleaseBaseURL), "/")
	if base == "" {
		return OutcomeNoop, fmt.Errorf("no release base URL provided")
	}
	// The release tarball contains exactly one member named after the
	// archive basename (see .github/workflows/release.yml: tar czf
	// "${BINARY}.tar.gz" "${BINARY}").
	binaryName := fmt.Sprintf("panvex-agent-linux-%s", runtime.GOARCH)
	archiveName := binaryName + ".tar.gz"
	archiveURL := base + "/" + archiveName
	checksumURL := archiveURL + ".sha256"

	updatehosts.WarnIfNonDefault(ctx, logger, "agent self-update", archiveURL)

	logger.Info("agent self-update: downloading", "version", payload.Version, "url", archiveURL)

	archivePath, err := downloadToTemp(ctx, archiveURL, cfg)
	if err != nil {
		return OutcomeNoop, fmt.Errorf("download: %w", err)
	}

	checksumBytes, err := downloadBytes(ctx, checksumURL, defaultMaxChecksum, cfg)
	if err != nil {
		_ = os.Remove(archivePath)
		return OutcomeNoop, fmt.Errorf("download checksum: %w", err)
	}
	expectedChecksum := parseChecksumSidecar(checksumBytes)
	if expectedChecksum == "" {
		_ = os.Remove(archivePath)
		return OutcomeNoop, fmt.Errorf("verify: checksum sidecar is empty or malformed")
	}
	if err := verifyChecksum(archivePath, expectedChecksum); err != nil {
		_ = os.Remove(archivePath)
		return OutcomeNoop, fmt.Errorf("verify: %w", err)
	}

	binaryPath, err := extractBinaryFromArchive(archivePath, binaryName)
	_ = os.Remove(archivePath)
	if err != nil {
		return OutcomeNoop, fmt.Errorf("extract: %w", err)
	}

	currentPath, err := os.Executable()
	if err != nil {
		_ = os.Remove(binaryPath)
		return OutcomeNoop, fmt.Errorf("resolve executable: %w", err)
	}

	if err := replaceSelf(currentPath, binaryPath); err != nil {
		_ = os.Remove(binaryPath)
		return OutcomeNoop, fmt.Errorf("replace: %w", err)
	}

	// replaceBinary renames tmpPath into place, so no cleanup needed.
	// A3: do NOT restart (and never os.Exit) here — this runs inside the
	// job handler, before the JobResult is flushed to the panel. The caller
	// schedules the restart after handing the result off.
	logger.Info("agent self-update: binary replaced; awaiting scheduled restart", "version", payload.Version)
	return OutcomeUpdated, nil
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

// extractBinaryFromArchive scans the tarball for the release member
// wantName (a regular file; leading directories in the entry name are
// tolerated) and extracts it to an executable temp file. The archive
// checksum was verified BEFORE extraction, so the residual risks are
// structural: the wanted entry missing (README-first / repacked
// archives used to be extracted blindly, audit #9c) and a silently
// truncated copy.
func extractBinaryFromArchive(archivePath, wantName string) (string, error) {
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

	const maxBinarySize = 256 << 20 // 256 MB

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("archive does not contain %q", wantName)
		}
		if err != nil {
			return "", fmt.Errorf("read tar entry: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != wantName {
			continue
		}
		if hdr.Size <= 0 || hdr.Size > maxBinarySize {
			return "", fmt.Errorf("refusing to extract %q: declared size %d out of bounds (cap %d)", hdr.Name, hdr.Size, int64(maxBinarySize))
		}
		return extractTarEntry(tr, hdr)
	}
}

// extractTarEntry copies exactly hdr.Size bytes of the current tar
// entry into an executable temp file. Any size mismatch is fatal —
// a truncated binary must never reach the swap (mirrors the streamed
// size check in download.go's downloadToTemp).
func extractTarEntry(tr *tar.Reader, hdr *tar.Header) (string, error) {
	tmp, err := os.CreateTemp("", "panvex-agent-binary-*")
	if err != nil {
		return "", fmt.Errorf("create temp binary: %w", err)
	}
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}

	written, err := io.Copy(tmp, io.LimitReader(tr, hdr.Size+1))
	if err != nil {
		cleanup()
		return "", fmt.Errorf("extract binary: %w", err)
	}
	if written != hdr.Size {
		cleanup()
		return "", fmt.Errorf("extract binary: wrote %d bytes, tar header declares %d", written, hdr.Size)
	}
	if err := os.Chmod(tmp.Name(), 0o700); err != nil { //nolint:gosec // G302: 0o700 keeps the binary owner-only but still needs +x to run
		cleanup()
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

// RemoveBackup deletes the "<executable>.bak" left behind by a previous
// self-update swap (replaceSelf). cmd/agent calls it once the (possibly
// just-updated) binary has proven healthy — i.e. after the first
// successful panel sync — so an operator keeps the .bak for manual
// rollback while the new binary has not yet demonstrated it can reach
// the panel. Best-effort: a missing backup is the normal case.
func RemoveBackup(logger *slog.Logger) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	backup := exe + ".bak"
	if err := os.Remove(backup); err != nil {
		if !os.IsNotExist(err) && logger != nil {
			logger.Warn("self-update: failed to remove stale backup", "path", backup, "error", err)
		}
		return
	}
	if logger != nil {
		logger.Info("self-update: removed stale backup after healthy start", "path", backup)
	}
}
