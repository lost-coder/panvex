package telemtupdate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/agent/telemtrestart"
	"golang.org/x/sys/unix"
)

// Payload is the JSON payload of a telemt.update job. The panel serializes
// exactly these keys (contract for Task 11's job producer and Task 12's
// agent-side job handler). Mirrors internal/agent/updater.Payload's shape:
// the panel supplies the release directory and target version, the agent
// resolves its own architecture/libc via ResolveAsset so the panel can never
// pick the wrong asset.
type Payload struct {
	Version        string `json:"version"`
	ReleaseBaseURL string `json:"release_base_url"`
	// AllowDowngrade lets an operator install an older binary on purpose
	// (e.g. emergency rollback). Without it Execute refuses any version
	// below the running one.
	AllowDowngrade bool `json:"allow_downgrade,omitempty"`
	// RestartSpec is parsed by telemtrestart.New — see internal/restartspec
	// for the accepted strategies (systemd/procd/openrc/runit/command).
	RestartSpec string `json:"restart_spec"`
	// BinaryPath is the path to the currently-running Telemt binary. Execute
	// stages downloads in filepath.Dir(BinaryPath) so the final swap stays
	// on one filesystem (see swapBinary's doc comment).
	BinaryPath string `json:"binary_path"`
	// AssetFlavor selects a CPU-feature-level release variant (currently
	// only "v3" on x86_64); "" means the architecture baseline. Passed
	// through to ResolveAsset verbatim.
	AssetFlavor string `json:"asset_flavor,omitempty"`
}

// TelemtInfo is the subset of *telemt.Client the health-gate needs,
// satisfied structurally so tests can supply a fake without standing up an
// httptest server.
type TelemtInfo interface {
	HealthReady(ctx context.Context) (bool, string, error)
	FetchSystemInfo(ctx context.Context) (telemt.SystemInfo, error)
}

// Outcome reports what Execute actually did, for the job handler (Task 12)
// to turn into a JobResult.
type Outcome struct {
	// Updated is true when the binary was replaced and the health-gate
	// confirmed the new version is actually serving.
	Updated bool
	// Message is a human-readable summary, suitable for JobResult text.
	Message string
	// KeepBackup is true when Execute could not fully recover on its own
	// (rollback restart also failed) and left binaryPath+backupSuffix in
	// place for manual recovery instead of removing or relying on it.
	KeepBackup bool
}

// Tuning overrides the health-gate timing. Production always uses
// defaultTuning(); tests shrink both fields well below their production
// values (Tuning{HealthTimeout: 100 * time.Millisecond, ...}) so the
// health-gate's poll loop doesn't make the suite slow.
type Tuning struct {
	HealthTimeout      time.Duration
	HealthPollInterval time.Duration
}

const (
	defaultHealthTimeout      = 60 * time.Second
	defaultHealthPollInterval = 2 * time.Second
)

func defaultTuning() Tuning {
	return Tuning{HealthTimeout: defaultHealthTimeout, HealthPollInterval: defaultHealthPollInterval}
}

// Execute performs a full Telemt binary update: downgrade gate, download,
// checksum verify, extract, atomic swap, restart, and a post-restart
// health-gate that rolls the swap back (and retries the restart) if the new
// binary never proves itself healthy.
//
// currentVersion is the version Telemt reports right now (the caller reads
// it via TelemtInfo.FetchSystemInfo before calling Execute, so the running
// process and currentVersion agree) — compared against p.Version so a panel
// cannot silently roll Telemt back to a vulnerable past release. See
// checkDowngrade.
//
// Execute never restarts Telemt on its own initiative outside the flow
// described above: a restart happens exactly once after the swap, and at
// most once more if the health-gate times out and the binary is rolled
// back.
func Execute(ctx context.Context, p Payload, currentVersion string, info TelemtInfo,
	runner telemtrestart.CommandRunner, logger *slog.Logger) (Outcome, error) {
	return executeWith(ctx, p, currentVersion, info, runner, logger, defaultConfig(), defaultTuning())
}

// executeWith is the testable form: the download policy and health-gate
// timing come from cfg/tuning instead of hard-coded production defaults.
func executeWith(ctx context.Context, p Payload, currentVersion string, info TelemtInfo,
	runner telemtrestart.CommandRunner, logger *slog.Logger, cfg Config, tuning Tuning) (Outcome, error) {
	// Downgrade gate first, before any network access — a compromised or
	// mistaken panel pinning Telemt to an older release is rejected before
	// we ever touch the network or the filesystem.
	if err := checkDowngrade(currentVersion, p.Version, p.AllowDowngrade); err != nil {
		if errors.Is(err, errAlreadyAtTarget) {
			logger.InfoContext(ctx, "telemtupdate: already at target version", "version", currentVersion)
			return Outcome{Message: fmt.Sprintf("already at target version %s", currentVersion)}, nil
		}
		return Outcome{}, err
	}

	assetName, err := ResolveAsset(p.AssetFlavor)
	if err != nil {
		return Outcome{}, fmt.Errorf("resolve asset: %w", err)
	}

	base := strings.TrimRight(strings.TrimSpace(p.ReleaseBaseURL), "/")
	if base == "" {
		return Outcome{}, fmt.Errorf("no release base URL provided")
	}
	archiveURL := base + "/" + assetName + ".tar.gz"
	checksumURL := archiveURL + ".sha256"
	destDir := filepath.Dir(p.BinaryPath)

	logger.InfoContext(ctx, "telemtupdate: downloading", "from", currentVersion, "to", p.Version, "url", archiveURL)

	archivePath, sha256hex, err := fetchToTemp(ctx, cfg, archiveURL, destDir)
	if err != nil {
		return Outcome{}, fmt.Errorf("download: %w", err)
	}

	// Conservative preflight: we don't know the decompressed size until
	// extraction, so budget 3x the compressed archive size (spec §4.5) and
	// check it before spending the CPU/IO on extraction.
	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		_ = os.Remove(archivePath)
		return Outcome{}, fmt.Errorf("stat downloaded archive: %w", err)
	}
	need := uint64(archiveInfo.Size()) * 3 //nolint:gosec // G115: archive size is bounded by cfg.MaxArchive (128 MiB default), always non-negative.
	if err := preflightSpace(destDir, need, unix.Statfs); err != nil {
		_ = os.Remove(archivePath)
		return Outcome{}, fmt.Errorf("preflight: %w", err)
	}

	sidecar, err := fetchChecksum(ctx, cfg, checksumURL)
	if err != nil {
		_ = os.Remove(archivePath)
		return Outcome{}, fmt.Errorf("download checksum: %w", err)
	}
	// Compare strictly against sha256hex as returned by fetchToTemp (hashed
	// while streaming the download) rather than re-reading the archive from
	// disk — re-reading would open a TOCTOU window between the verified
	// bytes and whatever extractBinary later reads. The sidecar itself must
	// first look like a real digest: a malformed or truncated sidecar must
	// fail closed with a clear error instead of comparing unequal lengths.
	normalized := strings.ToLower(sidecar)
	if !isHex64(normalized) {
		_ = os.Remove(archivePath)
		return Outcome{}, fmt.Errorf("malformed checksum sidecar: %q is not a 64-character hex digest", sidecar)
	}
	if normalized != sha256hex {
		_ = os.Remove(archivePath)
		return Outcome{}, fmt.Errorf("checksum mismatch: got %s, want %s", sha256hex, normalized)
	}

	binTmpPath, _, err := extractBinary(archivePath, destDir)
	_ = os.Remove(archivePath)
	if err != nil {
		return Outcome{}, fmt.Errorf("extract: %w", err)
	}

	// Precondition of swapBinary: p.BinaryPath must be the binary of the
	// currently running Telemt process. Execute guarantees this because
	// currentVersion (and thus p.BinaryPath) is only ever supplied by a
	// caller that just read it from the live process.
	backupPath, err := swapBinary(binTmpPath, p.BinaryPath)
	if err != nil {
		_ = os.Remove(binTmpPath)
		return Outcome{}, fmt.Errorf("swap: %w", err)
	}

	restarter, err := telemtrestart.New(p.RestartSpec, runner)
	if err == nil {
		err = restarter.Restart(ctx)
	}
	if err != nil {
		return restoreAfterRestartFailure(ctx, logger, p.BinaryPath, backupPath, currentVersion, err)
	}

	logger.InfoContext(ctx, "telemtupdate: restarted, waiting for health confirmation", "target_version", p.Version)

	if pollHealth(ctx, info, p.Version, tuning) {
		if rmErr := removeBackup(p.BinaryPath); rmErr != nil {
			logger.WarnContext(ctx, "telemtupdate: failed to remove backup after healthy update", "path", backupPath, "error", rmErr)
		}
		logger.InfoContext(ctx, "telemtupdate: update confirmed healthy", "from", currentVersion, "to", p.Version)
		return Outcome{Updated: true, Message: fmt.Sprintf("telemt updated %s → %s", currentVersion, p.Version)}, nil
	}

	// Health never confirmed the target version within tuning.HealthTimeout
	// (or ctx was cancelled mid-poll, which pollHealth treats the same way)
	// — roll the swap back and get Telemt running again on the old binary.
	logger.WarnContext(ctx, "telemtupdate: health gate did not confirm target version, rolling back", "target_version", p.Version)

	if rerr := restoreBackup(p.BinaryPath); rerr != nil {
		// restoreBackup itself failed: a failed rename leaves both its
		// source and destination exactly as they were, so backupPath
		// genuinely still holds the pre-update binary on disk. The binary
		// in place at p.BinaryPath is still the unhealthy new one, so there
		// is nothing safe to restart into — do not attempt a rollback
		// Restart here.
		msg := fmt.Sprintf("manual recovery required, backup kept at %s: %v", backupPath, rerr)
		logger.ErrorContext(ctx, "telemtupdate: restore backup failed after health-gate timeout", "error", rerr)
		return Outcome{KeepBackup: true, Message: msg}, fmt.Errorf("health check failed and restore backup failed: %w", rerr)
	}

	// restoreBackup succeeded: p.BinaryPath now holds the old binary again,
	// and backupPath (the .bak it renamed from) is gone — consumed by that
	// rename. No message from this point on can truthfully claim a backup
	// is being kept.
	//
	// The incoming ctx may already be cancelled or past its deadline — that
	// is precisely what routes here when the caller's context is cancelled
	// mid-poll. Strip cancellation (keeping any request-scoped values) for
	// the rollback restart so a caller-side timeout does not also doom the
	// one attempt we have left to get Telemt running again.
	rollbackCtx := context.WithoutCancel(ctx)
	if rerr := restarter.Restart(rollbackCtx); rerr != nil {
		msg := fmt.Sprintf("old binary restored to %s but telemt was not restarted (rollback restart failed: %v); manually restart the service", p.BinaryPath, rerr)
		logger.ErrorContext(ctx, "telemtupdate: rollback restart failed after binary was restored", "error", rerr)
		return Outcome{Message: msg}, fmt.Errorf("update failed health check and rollback restart failed: %w", rerr)
	}

	logger.InfoContext(ctx, "telemtupdate: rolled back after failed health check", "version", currentVersion)
	return Outcome{Message: fmt.Sprintf("update failed health check; rolled back to %s", currentVersion)},
		fmt.Errorf("update failed health check: telemt did not report version %q within %s", p.Version, tuning.HealthTimeout)
}

// restoreAfterRestartFailure handles the "restart command itself failed"
// branch: the binary was already swapped in, but the post-swap restart
// (telemtrestart.New or Restart) errored before Telemt ever came back up
// under the new binary. Recovery is a single restoreBackup — there is
// nothing running yet to roll back via a second restart.
func restoreAfterRestartFailure(ctx context.Context, logger *slog.Logger, binaryPath, backupPath, currentVersion string, restartErr error) (Outcome, error) {
	if rerr := restoreBackup(binaryPath); rerr != nil {
		msg := fmt.Sprintf("update failed AND restore backup failed: %v (restart error: %v); manual recovery required, backup kept at %s", rerr, restartErr, backupPath)
		logger.ErrorContext(ctx, "telemtupdate: restore backup failed after restart failure", "restore_error", rerr, "restart_error", restartErr)
		return Outcome{KeepBackup: true, Message: msg}, fmt.Errorf("restart failed: %w (restore backup also failed: %w)", restartErr, rerr)
	}
	logger.ErrorContext(ctx, "telemtupdate: restart failed, binary restored", "error", restartErr)
	msg := fmt.Sprintf("restart command failed: %v; binary restored, telemt untouched (still running %s)", restartErr, currentVersion)
	return Outcome{Message: msg}, fmt.Errorf("restart failed: %w", restartErr)
}

// pollHealth polls TelemtInfo until it reports Ready with the target
// version, or tuning.HealthTimeout / ctx cancellation cuts the loop short.
// The first check happens immediately (no up-front sleep), then every
// tuning.HealthPollInterval.
func pollHealth(ctx context.Context, info TelemtInfo, targetVersion string, tuning Tuning) bool {
	deadline := time.Now().Add(tuning.HealthTimeout)
	for {
		if ready, _, err := info.HealthReady(ctx); err == nil && ready {
			if sysInfo, sysErr := info.FetchSystemInfo(ctx); sysErr == nil && sysInfo.Version == targetVersion {
				return true
			}
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		wait := tuning.HealthPollInterval
		if wait <= 0 || wait > remaining {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

// isHex64 reports whether s is exactly 64 lowercase hex characters — the
// shape of a sha256 digest. Callers normalize to lowercase before calling.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
