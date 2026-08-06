package telemtupdate

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// backupSuffix names the pre-swap backup that swapBinary leaves next to
// binaryPath (binaryPath+backupSuffix), and the one restoreBackup and
// removeBackup operate on.
const backupSuffix = ".bak"

// preflightSpace checks that the filesystem holding dir has at least need
// bytes available to an unprivileged writer before the caller downloads or
// extracts anything into it — the fetch/extract path in download.go stages
// its temp files in the binary's own directory (so the final rename stays
// on one filesystem), so dir here is that same directory.
//
// Availability is computed from Bavail (blocks available to a
// non-privileged process), not Bfree (blocks free including the
// root-reserved slice) — the agent should not report success and then hit
// ENOSPC because the last few percent of the filesystem was reserved for
// root. statfs is injected so tests can supply a fake Statfs_t without
// needing a filesystem near capacity; production callers pass unix.Statfs.
func preflightSpace(dir string, need uint64, statfs func(path string, buf *unix.Statfs_t) error) error {
	var st unix.Statfs_t
	if err := statfs(dir, &st); err != nil {
		return fmt.Errorf("statfs %q: %w", dir, err)
	}
	avail := st.Bavail * uint64(st.Bsize) //nolint:gosec // G115: Bsize is the kernel-reported block size, always non-negative.
	if avail < need {
		const mib = 1 << 20
		return fmt.Errorf("insufficient disk space: need %d MB, have %d MB", need/mib, avail/mib)
	}
	return nil
}

// swapBinary atomically installs binTmpPath as binaryPath, keeping a backup
// of the previous binary at binaryPath+backupSuffix so a failed post-swap
// health check can restoreBackup. Callers (Task 3's extractBinary /
// stageMember) stage binTmpPath in binaryPath's own directory specifically
// so both renames below stay on one filesystem and are therefore atomic —
// swapBinary does not itself guard against a cross-filesystem EXDEV.
//
// Precondition: binaryPath must be the binary of the currently *running*
// Telemt process — i.e. the caller (Execute) has already gone to the
// Telemt API for the running version before reaching the swap step. This
// matters because swapBinary silently overwrites any pre-existing
// binaryPath+backupSuffix rather than refusing when one is already there.
// A stale .bak can exist if a previous update crashed between swapBinary
// and removeBackup; but because the precondition guarantees binaryPath is
// always the live, working binary at swap time, it is by construction
// newer/more trustworthy than any stale backup sitting next to it —
// overwriting the stale .bak with it is an improvement to the "last known
// good" backup, not a loss of one. Refusing instead would only block
// legitimate updates behind a leftover file from an update that never even
// reached the point of being a risk.
//
// Permissions and ownership are copied from the binary being replaced. This
// is MVP semantics, not a general-purpose install step: swapBinary chmods
// binTmpPath to the current binary's file mode, and best-effort chowns it
// to the current binary's uid/gid. The chown is deliberately not fatal —
// the agent is not guaranteed to run as root (rootless / non-root
// deployments), chown then fails with EPERM, and there is no logger
// available in this package to record the miss; refusing to update over an
// ownership detail that already held before the swap would be a worse
// outcome than leaving ownership unchanged.
//
// On failure, any partial rename is rolled back so binaryPath is left
// exactly as it was found; in particular, if binaryPath does not exist yet
// (nothing to back up), swapBinary returns an error before renaming
// anything.
func swapBinary(binTmpPath, binaryPath string) (backupPath string, err error) {
	return swapBinaryWith(binTmpPath, binaryPath, os.Rename)
}

// swapBinaryWith is swapBinary with the os.Rename calls routed through an
// injectable renameFn, so tests can force the second rename to fail (and so
// exercise the rollback-the-first-rename branch) without needing a real
// OS-level fault such as a read-only mount partway through the swap.
// swapBinary — the package's real entry point, matching the brief's
// signature — always calls this with os.Rename.
func swapBinaryWith(binTmpPath, binaryPath string, renameFn func(oldpath, newpath string) error) (backupPath string, err error) {
	info, err := os.Stat(binaryPath)
	if err != nil {
		return "", fmt.Errorf("stat current binary %q: %w", binaryPath, err)
	}

	if err := os.Chmod(binTmpPath, info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("chmod new binary: %w", err)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(binTmpPath, int(st.Uid), int(st.Gid)) // best-effort, see doc comment
	}

	// renameFn(binaryPath, backupPath) overwrites any pre-existing
	// binaryPath+backupSuffix — see the "stale backup" precondition note
	// in swapBinary's doc comment above.
	backupPath = binaryPath + backupSuffix
	if err := renameFn(binaryPath, backupPath); err != nil {
		return "", fmt.Errorf("back up current binary: %w", err)
	}

	if err := renameFn(binTmpPath, binaryPath); err != nil {
		if rerr := renameFn(backupPath, binaryPath); rerr != nil {
			return "", fmt.Errorf("install new binary: %w (rollback also failed: %w)", err, rerr)
		}
		return "", fmt.Errorf("install new binary: %w", err)
	}

	return backupPath, nil
}

// restoreBackup renames binaryPath+backupSuffix back to binaryPath, undoing
// a prior successful swapBinary. Callers use this when the newly installed
// binary fails a post-swap health check.
func restoreBackup(binaryPath string) error {
	if err := os.Rename(binaryPath+backupSuffix, binaryPath); err != nil {
		return fmt.Errorf("restore backup: %w", err)
	}
	return nil
}

// removeBackup deletes binaryPath+backupSuffix once the newly installed
// binary has been confirmed healthy and the backup is no longer needed.
func removeBackup(binaryPath string) error {
	if err := os.Remove(binaryPath + backupSuffix); err != nil {
		return fmt.Errorf("remove backup: %w", err)
	}
	return nil
}
