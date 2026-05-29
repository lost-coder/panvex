package integrations_test

import (
	"encoding/json"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/fleet/integrations"
)

func TestCloudflareProviderMetadata(t *testing.T) {
	p := integrations.NewCloudflareProvider()
	if p.Name() != "cloudflare-provider" {
		t.Fatalf("Name() = %q, want cloudflare-provider", p.Name())
	}
	if p.Description() == "" {
		t.Fatal("Description() is empty")
	}
	secrets := p.SecretFields()
	if len(secrets) != 1 || secrets[0] != "api_token" {
		t.Fatalf("SecretFields() = %v, want [api_token]", secrets)
	}
}

func TestCloudflareProviderValidate(t *testing.T) {
	p := integrations.NewCloudflareProvider()
	cases := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{"valid", `{"api_token":"tok","account_id":"acc"}`, false},
		{"empty config", ``, true},
		{"empty object", `{}`, true},
		{"missing api_token", `{"account_id":"acc"}`, true},
		{"blank api_token", `{"api_token":"","account_id":"acc"}`, true},
		{"missing account_id", `{"api_token":"tok"}`, true},
		{"unknown field", `{"api_token":"tok","account_id":"acc","zone":"x"}`, true},
		{"malformed json", `{"api_token":`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := p.Validate(json.RawMessage(tc.config))
			if tc.wantErr && err == nil {
				t.Fatalf("Validate(%s) = nil, want error", tc.config)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate(%s) = %v, want nil", tc.config, err)
			}
		})
	}
}
