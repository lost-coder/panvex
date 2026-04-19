package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestRequestContractFixtures ensures every canonical request-body fixture
// in testdata/api/requests.json decodes into the corresponding Go struct
// under strict field-matching (DisallowUnknownFields). The same fixtures
// are replayed through the frontend's Zod schemas by
// web/src/shared/api/schemas/requests/contracts.test.ts, so any drift on
// either side — a new optional field the backend accepts but the schema
// rejects, a renamed JSON tag, etc. — fails exactly one side of the
// contract test and surfaces immediately in CI.
func TestRequestContractFixtures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "api", "requests.json"))
	if err != nil {
		t.Fatalf("read fixture file: %v", err)
	}

	var bundle map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode fixture bundle: %v", err)
	}

	type decoder struct {
		name    string
		newZero func() any
	}
	decoders := []decoder{
		{"loginRequest", func() any { return new(loginRequest) }},
		{"updateTotpRequest", func() any { return new(updateTotpRequest) }},
		{"createUserRequest", func() any { return new(createUserRequest) }},
		{"updateUserRequest", func() any { return new(updateUserRequest) }},
		{"clientMutationRequest", func() any { return new(clientMutationRequest) }},
		{"renameAgentRequest", func() any { return new(renameAgentRequest) }},
		{"createEnrollmentTokenRequest", func() any { return new(createEnrollmentTokenRequest) }},
		{"createJobRequest", func() any { return new(createJobRequest) }},
		{"updateAppearanceSettingsRequest", func() any { return new(updateAppearanceSettingsRequest) }},
		{"updatePanelSettingsRequest", func() any { return new(updatePanelSettingsRequest) }},
		{"panelUpdateRequest", func() any { return new(panelUpdateRequest) }},
		{"updateSettingsRequest", func() any { return new(updateSettingsRequest) }},
		{"agentBootstrapRequest", func() any { return new(agentBootstrapRequest) }},
		{"agentCertificateRecoveryRequest", func() any { return new(agentCertificateRecoveryRequest) }},
		{"agentCertificateRecoveryGrantRequest", func() any { return new(agentCertificateRecoveryGrantRequest) }},
	}

	for _, d := range decoders {
		variants, ok := bundle[d.name]
		if !ok {
			t.Errorf("fixture bundle missing entry for %q", d.name)
			continue
		}
		if len(variants) == 0 {
			t.Errorf("fixture %q has no variants (expected at least \"full\")", d.name)
			continue
		}
		for variantName, payload := range variants {
			variantName, payload := variantName, payload
			t.Run(d.name+"/"+variantName, func(t *testing.T) {
				target := d.newZero()
				dec := json.NewDecoder(bytes.NewReader(payload))
				dec.DisallowUnknownFields()
				if err := dec.Decode(target); err != nil {
					t.Fatalf("decode %s/%s: %v\npayload: %s", d.name, variantName, err, payload)
				}
			})
		}
	}
}
