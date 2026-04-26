package config

import (
	"errors"
	"testing"
)

func TestValidateStorageSecurityRejectsEmptyPostgresPassword(t *testing.T) {
	t.Setenv(EnvDBPassword, "")
	t.Setenv(EnvAllowEmptyDBPassword, "")
	t.Setenv(EnvAllowInsecureDB, "1") // sidestep the unrelated SSL guard

	cases := []struct {
		name string
		dsn  string
	}{
		{"no-userinfo", "postgres://db.internal:5432/panvex?sslmode=disable"},
		{"empty-password-explicit", "postgres://panvex:@db.internal:5432/panvex?sslmode=disable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateStorageSecurity(StorageConfig{Driver: StorageDriverPostgres, DSN: tc.dsn})
			if !errors.Is(err, ErrEmptyPostgresPassword) {
				t.Fatalf("ValidateStorageSecurity() error = %v, want ErrEmptyPostgresPassword", err)
			}
		})
	}
}

func TestValidateStorageSecurityAllowsEmptyPasswordWhenEnvSet(t *testing.T) {
	t.Setenv(EnvDBPassword, "")
	t.Setenv(EnvAllowEmptyDBPassword, "1")
	t.Setenv(EnvAllowInsecureDB, "1")

	if err := ValidateStorageSecurity(StorageConfig{
		Driver: StorageDriverPostgres,
		DSN:    "postgres://panvex@db.internal:5432/panvex?sslmode=disable",
	}); err != nil {
		t.Fatalf("ValidateStorageSecurity() error = %v, want nil with PANVEX_ALLOW_EMPTY_DB_PASSWORD=1", err)
	}
}

func TestValidateStorageSecurityAllowsPasswordViaEnv(t *testing.T) {
	t.Setenv(EnvDBPassword, "from-env")
	t.Setenv(EnvAllowEmptyDBPassword, "")
	t.Setenv(EnvAllowInsecureDB, "1")

	// PANVEX_DB_PASSWORD covers the gap when the URL omits a password.
	if err := ValidateStorageSecurity(StorageConfig{
		Driver: StorageDriverPostgres,
		DSN:    "postgres://panvex@db.internal:5432/panvex?sslmode=disable",
	}); err != nil {
		t.Fatalf("ValidateStorageSecurity() error = %v, want nil with PANVEX_DB_PASSWORD set", err)
	}
}

func TestValidateStorageSecurityIgnoresKeywordDSN(t *testing.T) {
	t.Setenv(EnvDBPassword, "")
	t.Setenv(EnvAllowEmptyDBPassword, "")
	// keyword DSN — password may live in PGPASSWORD or .pgpass, which we
	// can't reliably inspect, so do not block.
	if err := ValidateStorageSecurity(StorageConfig{
		Driver: StorageDriverPostgres,
		DSN:    "host=db.internal user=panvex dbname=panvex sslmode=require",
	}); err != nil {
		t.Fatalf("ValidateStorageSecurity() keyword DSN error = %v, want nil", err)
	}
}
