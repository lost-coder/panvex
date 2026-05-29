package fleet_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/fleet"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// stubProviderKind is a no-op provider kind so CreateProvider's Validate
// passes without a real integration backend.
type stubProviderKind struct{}

func (stubProviderKind) Name() string                     { return "cf" }
func (stubProviderKind) Description() string              { return "test provider" }
func (stubProviderKind) SecretFields() []string           { return []string{"api_token"} }
func (stubProviderKind) Validate(_ json.RawMessage) error { return nil }

// TestProviderConfigEncryptedAtRest guards H-6: integration-provider
// credentials must be sealed at rest. The persisted row must not contain
// the plaintext secret, and a service read must transparently decrypt it.
func TestProviderConfigEncryptedAtRest(t *testing.T) {
	store, err := sqlite.Open(t.TempDir() + "/fleet.db")
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	vault, err := secretvault.New("test-passphrase", secretvault.AllDomains)
	if err != nil {
		t.Fatalf("secretvault.New() error = %v", err)
	}

	svc := fleet.NewService(store, func() time.Time {
		return time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	})
	svc.SetVault(vault)
	if err := svc.ProviderRegistry().Register(stubProviderKind{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	plaintextConfig := json.RawMessage(`{"api_token":"super-secret-token"}`)
	created, err := svc.CreateProvider(context.Background(), fleet.CreateProviderInput{
		Kind:   "cf",
		Label:  "acct",
		Config: plaintextConfig,
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	// The returned record carries plaintext for the caller.
	if string(created.Config) != string(plaintextConfig) {
		t.Fatalf("CreateProvider returned config %s, want plaintext", created.Config)
	}

	// The persisted row must be sealed, not plaintext.
	raw, err := store.GetIntegrationProvider(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("store.GetIntegrationProvider() error = %v", err)
	}
	if strings.Contains(string(raw.Config), "super-secret-token") {
		t.Fatalf("persisted config contains plaintext secret: %s", raw.Config)
	}
	if !secretvault.IsEncrypted(string(raw.Config)) {
		t.Fatalf("persisted config is not vault-encrypted: %s", raw.Config)
	}

	// A service read decrypts transparently.
	got, err := svc.GetProvider(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	if string(got.Config) != string(plaintextConfig) {
		t.Fatalf("GetProvider config = %s, want %s", got.Config, plaintextConfig)
	}
}
