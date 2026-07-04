package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runAuthorityContract extracts the certificate-authority contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runAuthorityContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("certificate authority round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		authority := storage.CertificateAuthorityRecord{
			CAPEM:         "ca-pem",
			PrivateKeyPEM: "ca-key-pem",
			UpdatedAt:     time.Date(2026, time.March, 16, 18, 10, 0, 0, time.UTC),
		}

		if err := store.PutCertificateAuthority(ctx, authority); err != nil {
			t.Fatalf("PutCertificateAuthority() error = %v", err)
		}

		stored, err := store.GetCertificateAuthority(ctx)
		if err != nil {
			t.Fatalf("GetCertificateAuthority() error = %v", err)
		}

		if stored.CAPEM != authority.CAPEM {
			t.Fatalf("GetCertificateAuthority() CAPEM = %q, want %q", stored.CAPEM, authority.CAPEM)
		}
		if stored.PrivateKeyPEM != authority.PrivateKeyPEM {
			t.Fatalf("GetCertificateAuthority() PrivateKeyPEM = %q, want %q", stored.PrivateKeyPEM, authority.PrivateKeyPEM)
		}
		if !stored.UpdatedAt.Equal(authority.UpdatedAt) {
			t.Fatalf("GetCertificateAuthority() UpdatedAt = %v, want %v", stored.UpdatedAt, authority.UpdatedAt)
		}
	})

}
