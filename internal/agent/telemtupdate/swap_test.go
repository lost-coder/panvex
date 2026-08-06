package telemtupdate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// ---- swapBinary ----

func TestSwapBinary_ReplacesContentAndLeavesBackup(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "telemt")
	if err := os.WriteFile(binaryPath, []byte("old-binary"), 0o600); err != nil {
		t.Fatal(err)
	}

	tmpPath := filepath.Join(dir, ".telemt-update-bin-new")
	if err := os.WriteFile(tmpPath, []byte("new-binary"), 0o600); err != nil {
		t.Fatal(err)
	}

	backupPath, err := swapBinary(tmpPath, binaryPath)
	if err != nil {
		t.Fatalf("swapBinary: %v", err)
	}
	if backupPath != binaryPath+backupSuffix {
		t.Fatalf("backupPath = %q, want %q", backupPath, binaryPath+backupSuffix)
	}

	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-binary" {
		t.Fatalf("binaryPath content = %q, want %q", got, "new-binary")
	}

	bak, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != "old-binary" {
		t.Fatalf("backup content = %q, want %q", bak, "old-binary")
	}

	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("tmpPath %q should have been consumed by rename, stat err = %v", tmpPath, err)
	}
}

func TestSwapBinary_CopiesPermissionsFromCurrentBinary(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "telemt")
	// 0o750 (not 0o600) is the point of this test: it must differ from the
	// new binary's own mode below so a passing test proves swapBinary
	// actually copied it, rather than merely preserving what was already
	// there.
	if err := os.WriteFile(binaryPath, []byte("old-binary"), 0o750); err != nil { //nolint:gosec // G306: intentional non-default mode, see comment above
		t.Fatal(err)
	}

	tmpPath := filepath.Join(dir, ".telemt-update-bin-new")
	if err := os.WriteFile(tmpPath, []byte("new-binary"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := swapBinary(tmpPath, binaryPath); err != nil {
		t.Fatalf("swapBinary: %v", err)
	}

	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("new binary mode = %v, want %v", info.Mode().Perm(), os.FileMode(0o750))
	}
}

func TestSwapBinary_ChownIsBestEffortAndDoesNotFail(t *testing.T) {
	// Non-root test process: chown to the old binary's (its own) uid/gid
	// should not error, and the swap must still succeed regardless of
	// whether the underlying chown syscall is permitted.
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "telemt")
	if err := os.WriteFile(binaryPath, []byte("old-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpPath := filepath.Join(dir, ".telemt-update-bin-new")
	if err := os.WriteFile(tmpPath, []byte("new-binary"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := swapBinary(tmpPath, binaryPath); err != nil {
		t.Fatalf("swapBinary: %v", err)
	}
}

func TestSwapBinary_MissingBinaryPathFailsWithoutSideEffects(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "telemt") // never created

	tmpPath := filepath.Join(dir, ".telemt-update-bin-new")
	if err := os.WriteFile(tmpPath, []byte("new-binary"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := swapBinary(tmpPath, binaryPath)
	if err == nil {
		t.Fatal("expected error when binaryPath does not exist")
	}

	if _, statErr := os.Stat(binaryPath); !os.IsNotExist(statErr) {
		t.Fatalf("binaryPath should still not exist, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(binaryPath + backupSuffix); !os.IsNotExist(statErr) {
		t.Fatalf("no backup should have been created, stat err = %v", statErr)
	}
	got, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("tmpPath should be untouched: %v", err)
	}
	if string(got) != "new-binary" {
		t.Fatalf("tmpPath content changed: %q", got)
	}
}

// ---- restoreBackup / removeBackup ----

func TestRestoreBackup_RestoresOldBinary(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "telemt")
	if err := os.WriteFile(binaryPath, []byte("old-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpPath := filepath.Join(dir, ".telemt-update-bin-new")
	if err := os.WriteFile(tmpPath, []byte("new-binary"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := swapBinary(tmpPath, binaryPath); err != nil {
		t.Fatalf("swapBinary: %v", err)
	}

	if err := restoreBackup(binaryPath); err != nil {
		t.Fatalf("restoreBackup: %v", err)
	}

	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-binary" {
		t.Fatalf("binaryPath content = %q, want %q", got, "old-binary")
	}
	if _, err := os.Stat(binaryPath + backupSuffix); !os.IsNotExist(err) {
		t.Fatalf("backup should be consumed by restore, stat err = %v", err)
	}
}

func TestRestoreBackup_MissingBackupFails(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "telemt")
	if err := os.WriteFile(binaryPath, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := restoreBackup(binaryPath); err == nil {
		t.Fatal("expected error when no backup exists")
	}
}

func TestRemoveBackup_DeletesBackupFile(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "telemt")
	if err := os.WriteFile(binaryPath, []byte("old-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpPath := filepath.Join(dir, ".telemt-update-bin-new")
	if err := os.WriteFile(tmpPath, []byte("new-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	backupPath, err := swapBinary(tmpPath, binaryPath)
	if err != nil {
		t.Fatalf("swapBinary: %v", err)
	}

	if err := removeBackup(binaryPath); err != nil {
		t.Fatalf("removeBackup: %v", err)
	}
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed, stat err = %v", err)
	}
}

func TestRemoveBackup_MissingBackupFails(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "telemt")

	if err := removeBackup(binaryPath); err == nil {
		t.Fatal("expected error when no backup exists to remove")
	}
}

// ---- preflightSpace ----

func fakeStatfs(bavail, bsize uint64) func(path string, buf *unix.Statfs_t) error {
	return func(_ string, buf *unix.Statfs_t) error {
		buf.Bavail = bavail
		buf.Bsize = int64(bsize)
		return nil
	}
}

func TestPreflightSpace_EnoughSpaceReturnsNil(t *testing.T) {
	dir := t.TempDir()
	// 1000 blocks * 4096 bytes = ~3.9 MiB available.
	statfs := fakeStatfs(1000, 4096)

	if err := preflightSpace(dir, 1<<20 /* need 1 MiB */, statfs); err != nil {
		t.Fatalf("preflightSpace: %v", err)
	}
}

func TestPreflightSpace_InsufficientSpaceReturnsErrorWithMB(t *testing.T) {
	dir := t.TempDir()
	// 10 blocks * 4096 bytes = 40960 bytes available (~0 MB).
	statfs := fakeStatfs(10, 4096)

	err := preflightSpace(dir, 100<<20 /* need 100 MiB */, statfs)
	if err == nil {
		t.Fatal("expected error for insufficient space")
	}
	want := "insufficient disk space: need 100 MB, have 0 MB"
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}

func TestPreflightSpace_StatfsErrorIsPropagated(t *testing.T) {
	dir := t.TempDir()
	wantErr := errors.New("boom")
	statfs := func(_ string, _ *unix.Statfs_t) error { return wantErr }

	err := preflightSpace(dir, 1<<20, statfs)
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapping %v", err, wantErr)
	}
}
