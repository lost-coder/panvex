package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
)

// LoadUsersFile reads the local-auth bootstrap file from disk.
func LoadUsersFile(path string) ([]auth.User, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var users []auth.User
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, err
	}

	return users, nil
}

// SaveUsersFile persists the local-auth bootstrap file to disk.
func SaveUsersFile(path string, users []auth.User) error {
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}
