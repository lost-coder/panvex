package integrations

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// cloudflareProviderKindName is the stable slug for the Cloudflare
// credential provider. Used as the registry key and the API/storage
// `kind` value.
const cloudflareProviderKindName = "cloudflare-provider"

// cloudflareConfig is the JSON shape of a cloudflare-provider config
// blob. APIToken is the write-only secret; AccountID is a
// non-sensitive identifier returned verbatim in API responses.
type cloudflareConfig struct {
	APIToken  string `json:"api_token"`
	AccountID string `json:"account_id"`
}

// CloudflareProvider is the ProviderKind for a shared Cloudflare
// account. It only validates the credential bundle's shape — DNS calls
// and the Cloudflare API client live in a separate runtime layer.
type CloudflareProvider struct{}

// NewCloudflareProvider returns the registrable Cloudflare provider
// kind. Wire it at boot via ProviderRegistry().Register.
func NewCloudflareProvider() CloudflareProvider { return CloudflareProvider{} }

func (CloudflareProvider) Name() string { return cloudflareProviderKindName }

func (CloudflareProvider) Description() string {
	return "Cloudflare account credentials (API token + account ID) for DNS-backed integrations."
}

// SecretFields marks api_token as write-only. account_id is a plain
// identifier and is not redacted.
func (CloudflareProvider) SecretFields() []string { return []string{"api_token"} }

// Validate parses the config blob and enforces the contract: a
// non-empty api_token and account_id, and no unknown fields (strict to
// catch typos in operator-supplied JSON).
func (CloudflareProvider) Validate(config json.RawMessage) error {
	if len(bytes.TrimSpace(config)) == 0 {
		return errors.New("cloudflare-provider: config is required")
	}
	dec := json.NewDecoder(bytes.NewReader(config))
	dec.DisallowUnknownFields()
	var cfg cloudflareConfig
	if err := dec.Decode(&cfg); err != nil {
		return fmt.Errorf("cloudflare-provider: invalid config: %w", err)
	}
	if cfg.APIToken == "" {
		return errors.New("cloudflare-provider: api_token is required")
	}
	if cfg.AccountID == "" {
		return errors.New("cloudflare-provider: account_id is required")
	}
	return nil
}

// Compile-time assertion that CloudflareProvider satisfies the
// ProviderKind interface.
var _ ProviderKind = CloudflareProvider{}
