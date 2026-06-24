package config

import (
	"errors"
	"testing"
)

// TestValidateStorageSecurityAllowsUnixSocketDSN verifies that a Unix-socket
// DSN with sslmode=disable is accepted even under PANVEX_ENV=production: the
// connection never traverses the network, so the plaintext-DSN guard (S10/S4)
// does not apply. This is the single-host topology shipped in
// deploy/docker-compose.prod.yml.
func TestValidateStorageSecurityAllowsUnixSocketDSN(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
	}{
		{
			name: "url-form host param",
			dsn:  "postgres://panvex:secret@/panvex?host=/var/run/postgresql&sslmode=disable",
		},
		{
			name: "keyword-form host path",
			dsn:  "host=/var/run/postgresql user=panvex password=secret dbname=panvex sslmode=disable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PANVEX_ENV", "production")
			if err := ValidateStorageSecurity(StorageConfig{
				Driver: StorageDriverPostgres,
				DSN:    tc.dsn,
			}); err != nil {
				t.Fatalf("ValidateStorageSecurity() socket DSN error = %v, want nil", err)
			}
		})
	}
}

// TestValidateStorageSecurityStillRejectsTCPPlaintext is a guard against the
// socket exemption being too broad: a TCP DSN (no socket host) with
// sslmode=disable must still be rejected in production.
func TestValidateStorageSecurityStillRejectsTCPPlaintext(t *testing.T) {
	t.Setenv("PANVEX_ENV", "production")
	err := ValidateStorageSecurity(StorageConfig{
		Driver: StorageDriverPostgres,
		DSN:    "postgres://panvex:secret@postgres:5432/panvex?sslmode=disable",
	})
	if !errors.Is(err, ErrInsecureDBDSNProd) {
		t.Fatalf("ValidateStorageSecurity() TCP plaintext error = %v, want ErrInsecureDBDSNProd", err)
	}
}
