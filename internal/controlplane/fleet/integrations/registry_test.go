package integrations_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/fleet/integrations"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// stubKind is a minimal IntegrationKind used to exercise the registry
// without standing up a real integration. Validate returns a sentinel
// error when the config blob is the literal string "bad" so tests can
// assert the plumbing from Validate → Register → HTTP.
type stubKind struct {
	name         string
	providerKind string
}

func (k stubKind) Name() string        { return k.name }
func (k stubKind) Description() string { return "test kind" }
func (k stubKind) ProviderKind() string { return k.providerKind }
func (k stubKind) Validate(config json.RawMessage, provider *storage.IntegrationProviderRecord) error {
	if string(config) == `"bad"` {
		return errors.New("stub: invalid config")
	}
	return nil
}

type stubProviderKind struct{ name string }

func (k stubProviderKind) Name() string                          { return k.name }
func (k stubProviderKind) Description() string                   { return "test provider" }
func (k stubProviderKind) Validate(config json.RawMessage) error { return nil }

func TestIntegrationRegistryRegisterAndGet(t *testing.T) {
	r := integrations.NewIntegrationRegistry()
	if err := r.Register(stubKind{name: "alpha"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.Register(stubKind{name: "alpha"}); err == nil {
		t.Fatal("Register(duplicate) error = nil, want duplicate rejection")
	}
	k, ok := r.Get("alpha")
	if !ok || k.Name() != "alpha" {
		t.Fatalf("Get(alpha) = %v, ok=%v", k, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("Get(missing) returned ok=true, want false")
	}
}

func TestIntegrationRegistryValidateUnknownKind(t *testing.T) {
	r := integrations.NewIntegrationRegistry()
	err := r.Validate("missing", json.RawMessage(`{}`), nil)
	if !errors.Is(err, integrations.ErrUnknownKind) {
		t.Fatalf("Validate(missing) error = %v, want ErrUnknownKind", err)
	}
}

func TestIntegrationRegistryValidateProviderContract(t *testing.T) {
	r := integrations.NewIntegrationRegistry()
	// Inline-only kind: rejects a provider argument.
	if err := r.Register(stubKind{name: "inline"}); err != nil {
		t.Fatalf("Register(inline) error = %v", err)
	}
	err := r.Validate("inline", json.RawMessage(`{}`), &storage.IntegrationProviderRecord{Kind: "x"})
	if !errors.Is(err, integrations.ErrProviderNotApplicable) {
		t.Fatalf("Validate(inline,+provider) error = %v, want ErrProviderNotApplicable", err)
	}

	// Provider-bound kind: rejects missing provider AND mismatched kind.
	if err := r.Register(stubKind{name: "bound", providerKind: "cf"}); err != nil {
		t.Fatalf("Register(bound) error = %v", err)
	}
	err = r.Validate("bound", json.RawMessage(`{}`), nil)
	if !errors.Is(err, integrations.ErrProviderRequired) {
		t.Fatalf("Validate(bound,no-provider) error = %v, want ErrProviderRequired", err)
	}
	err = r.Validate("bound", json.RawMessage(`{}`), &storage.IntegrationProviderRecord{Kind: "other"})
	if !errors.Is(err, integrations.ErrProviderKindMismatch) {
		t.Fatalf("Validate(bound,wrong-kind) error = %v, want ErrProviderKindMismatch", err)
	}

	// Happy path: correct provider kind.
	err = r.Validate("bound", json.RawMessage(`{}`), &storage.IntegrationProviderRecord{Kind: "cf"})
	if err != nil {
		t.Fatalf("Validate(bound, correct provider) error = %v", err)
	}
}

func TestProviderRegistryValidateUnknownKind(t *testing.T) {
	r := integrations.NewProviderRegistry()
	err := r.Validate("missing", json.RawMessage(`{}`))
	if !errors.Is(err, integrations.ErrUnknownProviderKind) {
		t.Fatalf("Validate(missing) error = %v, want ErrUnknownProviderKind", err)
	}
}

func TestIntegrationRegistryListSortsByName(t *testing.T) {
	r := integrations.NewIntegrationRegistry()
	for _, name := range []string{"bravo", "alpha", "charlie"} {
		if err := r.Register(stubKind{name: name}); err != nil {
			t.Fatalf("Register(%s) error = %v", name, err)
		}
	}
	list := r.List()
	if len(list) != 3 || list[0].Name() != "alpha" || list[2].Name() != "charlie" {
		t.Fatalf("List() = %v, want sorted [alpha bravo charlie]", list)
	}
}

// Compile-time assertion that the stub satisfies the interfaces.
var (
	_ integrations.IntegrationKind = stubKind{}
	_ integrations.ProviderKind    = stubProviderKind{}
)
