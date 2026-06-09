package runtime

import (
	"fmt"
	"os"
	"path/filepath"
)

// backupConfigFile copies path to "<path>.panvex.bak" and returns the backup
// path. Used before a config patch so a failed restart can be rolled back.
func backupConfigFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read config for backup: %w", err)
	}
	backup := path + ".panvex.bak"
	if err := writeFileAtomic(backup, data, 0o600); err != nil {
		return "", fmt.Errorf("write config backup: %w", err)
	}
	return backup, nil
}

// restoreConfigFile atomically copies backup back over path.
func restoreConfigFile(backup, path string) error {
	data, err := os.ReadFile(backup)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	return writeFileAtomic(path, data, 0o600)
}

// writeFileAtomic writes data to a temp file in the same directory, fsyncs it,
// then renames it over path (crash-safe). Mirrors internal/agent/state/credentials.go.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".panvex-cfg-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
