package config

import (
	"errors"
	"testing"
)

func TestStorageResolveDefaultsToSQLite(t *testing.T) {
	storage, err := ResolveStorage("", "")
	if err != nil {
		t.Fatalf("ResolveStorage() error = %v", err)
	}

	if storage.Driver != StorageDriverSQLite {
		t.Fatalf("storage.Driver = %q, want %q", storage.Driver, StorageDriverSQLite)
	}

	if storage.DSN != DefaultSQLiteDSN {
		t.Fatalf("storage.DSN = %q, want %q", storage.DSN, DefaultSQLiteDSN)
	}
}

func TestStorageResolveAcceptsExplicitPostgres(t *testing.T) {
	storage, err := ResolveStorage("postgres", "postgres://panvex:secret@localhost:5432/panvex?sslmode=disable")
	if err != nil {
		t.Fatalf("ResolveStorage() error = %v", err)
	}

	if storage.Driver != StorageDriverPostgres {
		t.Fatalf("storage.Driver = %q, want %q", storage.Driver, StorageDriverPostgres)
	}

	if storage.DSN != "postgres://panvex:secret@localhost:5432/panvex?sslmode=disable" {
		t.Fatalf("storage.DSN = %q, want explicit postgres DSN", storage.DSN)
	}
}

func TestStorageResolveRejectsUnsupportedDriver(t *testing.T) {
	_, err := ResolveStorage("mysql", "panvex")
	if !errors.Is(err, ErrUnsupportedStorageDriver) {
		t.Fatalf("ResolveStorage() error = %v, want %v", err, ErrUnsupportedStorageDriver)
	}
}

func TestStorageResolveRejectsEmptyPostgresDSN(t *testing.T) {
	_, err := ResolveStorage("postgres", "")
	if !errors.Is(err, ErrStorageDSNRequired) {
		t.Fatalf("ResolveStorage() error = %v, want %v", err, ErrStorageDSNRequired)
	}
}

// TestValidateStorageSecurityProductionIgnoresEscapeHatches verifies S4:
// PANVEX_ENV=production disables the insecure-DB opt-ins
// (PANVEX_ALLOW_INSECURE_DB / PANVEX_ALLOW_EMPTY_DB_PASSWORD), while
// non-production keeps honouring them.
func TestValidateStorageSecurityProductionIgnoresEscapeHatches(t *testing.T) {
	tests := []struct {
		name    string
		env     string // PANVEX_ENV value
		dsn     string
		setEnvs map[string]string
		wantErr error
	}{
		{
			name:    "production with insecure optin is rejected",
			env:     "production",
			dsn:     "postgres://user:pw@localhost:5432/panvex?sslmode=disable",
			setEnvs: map[string]string{EnvAllowInsecureDB: "1"},
			wantErr: ErrInsecureDBDSNProd,
		},
		{
			name:    "production env is case-insensitive",
			env:     "Production",
			dsn:     "postgres://user:pw@localhost:5432/panvex?sslmode=disable",
			setEnvs: map[string]string{EnvAllowInsecureDB: "1"},
			wantErr: ErrInsecureDBDSNProd,
		},
		{
			name:    "non-production with insecure optin is allowed",
			env:     "development",
			dsn:     "postgres://user:pw@localhost:5432/panvex?sslmode=disable",
			setEnvs: map[string]string{EnvAllowInsecureDB: "1"},
			wantErr: nil,
		},
		{
			name:    "production with empty-password optin is rejected",
			env:     "production",
			dsn:     "postgres://user@localhost:5432/panvex?sslmode=require",
			setEnvs: map[string]string{EnvAllowEmptyDBPassword: "1"},
			wantErr: ErrEmptyPostgresPasswordProd,
		},
		{
			name:    "non-production with empty-password optin is allowed",
			env:     "",
			dsn:     "postgres://user@localhost:5432/panvex?sslmode=require",
			setEnvs: map[string]string{EnvAllowEmptyDBPassword: "1"},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PANVEX_ENV", tt.env)
			for k, v := range tt.setEnvs {
				t.Setenv(k, v)
			}

			err := ValidateStorageSecurity(StorageConfig{
				Driver: StorageDriverPostgres,
				DSN:    tt.dsn,
			})
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateStorageSecurity() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateStorageSecurity() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
