package config

import (
	"errors"
	"testing"
)

func TestValidateStorageSecurityKeywordForm(t *testing.T) {
	t.Setenv(EnvAllowInsecureDB, "")
	err := ValidateStorageSecurity(StorageConfig{
		Driver: StorageDriverPostgres,
		DSN:    "host=127.0.0.1 user=panvex password=secret dbname=panvex sslmode=disable",
	})
	if !errors.Is(err, ErrInsecureDBDSN) {
		t.Fatalf("keyword-form DSN with sslmode=disable: error = %v, want ErrInsecureDBDSN", err)
	}
}

func TestValidateStorageSecuritySQLiteIsUnaffected(t *testing.T) {
	t.Setenv(EnvAllowInsecureDB, "")
	if err := ValidateStorageSecurity(StorageConfig{
		Driver: StorageDriverSQLite,
		DSN:    "data/panvex.db",
	}); err != nil {
		t.Fatalf("sqlite DSN should never fail S10: %v", err)
	}
}
