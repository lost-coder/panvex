package fleet_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/fleet"
	"github.com/lost-coder/panvex/internal/controlplane/fleet/integrations"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// newCloudflareService builds a fleet.Service backed by a temp SQLite
// store with the real cloudflare-provider kind registered and an
// enabled vault, matching the production wiring.
func newCloudflareService(t *testing.T) *fleet.Service {
	t.Helper()
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
		return time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	})
	svc.SetVault(vault)
	if err := svc.ProviderRegistry().Register(integrations.NewCloudflareProvider()); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	return svc
}

// TestRedactProviderConfigBlanksSecret guards H-6: secret fields must be
// blanked to "" on read, while non-secret fields pass through.
func TestRedactProviderConfigBlanksSecret(t *testing.T) {
	svc := newCloudflareService(t)
	created, err := svc.CreateProvider(context.Background(), fleet.CreateProviderInput{
		Kind:   "cloudflare-provider",
		Label:  "acct",
		Config: json.RawMessage(`{"api_token":"super-secret","account_id":"acc-123"}`),
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	redacted := svc.RedactProviderConfig(created)
	var got map[string]string
	if err := json.Unmarshal(redacted, &got); err != nil {
		t.Fatalf("unmarshal redacted = %v", err)
	}
	if got["api_token"] != "" {
		t.Fatalf("api_token = %q, want blank", got["api_token"])
	}
	if got["account_id"] != "acc-123" {
		t.Fatalf("account_id = %q, want acc-123 (non-secret passthrough)", got["account_id"])
	}

	// GET path (decrypt + redact) must also hide the secret.
	fetched, err := svc.GetProvider(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	red2 := svc.RedactProviderConfig(fetched)
	if err := json.Unmarshal(red2, &got); err != nil {
		t.Fatalf("unmarshal redacted GetProvider config: %v", err)
	}
	if got["api_token"] != "" {
		t.Fatalf("GetProvider redacted api_token = %q, want blank", got["api_token"])
	}
}

// TestRedactProviderConfigUnknownKindFailSafe guards the fail-safe: an
// unregistered kind cannot identify its secrets, so the whole config is
// masked.
func TestRedactProviderConfigUnknownKindFailSafe(t *testing.T) {
	svc := newCloudflareService(t)
	rec := storage.IntegrationProviderRecord{
		Kind:   "totally-unknown-kind",
		Config: []byte(`{"api_token":"leaked","account_id":"acc"}`),
	}
	redacted := svc.RedactProviderConfig(rec)
	if string(redacted) != "{}" {
		t.Fatalf("RedactProviderConfig(unknown kind) = %s, want {}", redacted)
	}
}

// TestUpdateProviderKeepOnEmpty guards the write-only contract: an empty
// secret on update keeps the stored value; a non-empty secret overwrites
// it. Non-secret fields are taken from the input.
func TestUpdateProviderKeepOnEmpty(t *testing.T) {
	svc := newCloudflareService(t)
	created, err := svc.CreateProvider(context.Background(), fleet.CreateProviderInput{
		Kind:   "cloudflare-provider",
		Label:  "acct",
		Config: json.RawMessage(`{"api_token":"original-token","account_id":"acc-1"}`),
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	// Update with blank secret + changed label/account_id → keep token.
	updated, err := svc.UpdateProvider(context.Background(), created.ID, fleet.UpdateProviderInput{
		Label:  "renamed",
		Config: json.RawMessage(`{"api_token":"","account_id":"acc-2"}`),
	})
	if err != nil {
		t.Fatalf("UpdateProvider(blank secret) error = %v", err)
	}
	if updated.Label != "renamed" {
		t.Fatalf("Label = %q, want renamed", updated.Label)
	}
	// The returned (merged) plaintext must carry the original token and
	// the new account_id.
	var merged map[string]string
	if err := json.Unmarshal(updated.Config, &merged); err != nil {
		t.Fatalf("unmarshal merged = %v", err)
	}
	if merged["api_token"] != "original-token" {
		t.Fatalf("api_token = %q, want original-token (kept)", merged["api_token"])
	}
	if merged["account_id"] != "acc-2" {
		t.Fatalf("account_id = %q, want acc-2 (overwritten)", merged["account_id"])
	}

	// Confirm persistence: re-read decrypts the stored token.
	reread, err := svc.GetProvider(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	var stored map[string]string
	if err := json.Unmarshal(reread.Config, &stored); err != nil {
		t.Fatalf("unmarshal stored = %v", err)
	}
	if stored["api_token"] != "original-token" {
		t.Fatalf("persisted api_token = %q, want original-token", stored["api_token"])
	}

	// Update with a non-empty secret overwrites it.
	if _, err := svc.UpdateProvider(context.Background(), created.ID, fleet.UpdateProviderInput{
		Label:  "renamed",
		Config: json.RawMessage(`{"api_token":"new-token","account_id":"acc-2"}`),
	}); err != nil {
		t.Fatalf("UpdateProvider(new secret) error = %v", err)
	}
	reread2, err := svc.GetProvider(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	if err := json.Unmarshal(reread2.Config, &stored); err != nil {
		t.Fatalf("unmarshal stored2 = %v", err)
	}
	if stored["api_token"] != "new-token" {
		t.Fatalf("api_token = %q, want new-token (overwritten)", stored["api_token"])
	}
}
