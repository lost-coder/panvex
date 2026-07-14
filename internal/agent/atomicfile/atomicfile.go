// Package atomicfile writes a file so a crash mid-write can never leave a
// truncated or half-updated file behind. Both agent-side persisters (the
// Telemt config apply path and the credentials store) need exactly this, so
// the write lives here once.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write creates a temp file next to path, writes data, fsyncs it and renames
// it over path. The rename is atomic within a directory, so a reader either
// sees the old file or the complete new one. tmpPattern names the temp file
// (e.g. ".credentials-*.tmp") so a crash leaves an identifiable stray.
//
// The temp file is created in path's directory on purpose: a rename across
// filesystems is not atomic (and fails outright on most platforms).
func Write(path string, data []byte, perm os.FileMode, tmpPattern string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup; a no-op once the rename below succeeds.
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
