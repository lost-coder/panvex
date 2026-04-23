// Package integrations wires fleet-group integrations (e.g. DNS
// round-robin, webhooks, future plugins) to the control-plane. It
// provides two kinds of registries:
//
//   - IntegrationRegistry holds per-group integration kinds. An
//     IntegrationKind validates its installed config and declares
//     which shared provider it expects, if any.
//   - ProviderRegistry holds shared credential kinds (e.g. a
//     Cloudflare account). A ProviderKind validates provider config.
//
// Phase 3 (this package) ships the registry plumbing only. Concrete
// kinds (dns-rr, cloudflare-provider, …) register themselves in
// follow-up changes by calling Register at init or from an explicit
// wiring helper.
package integrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Errors returned by Validate* paths. HTTP handlers switch on these
// via errors.Is to map to status codes.
var (
	ErrUnknownKind           = errors.New("integration kind is not registered")
	ErrUnknownProviderKind   = errors.New("integration provider kind is not registered")
	ErrProviderRequired      = errors.New("integration kind requires a provider reference")
	ErrProviderKindMismatch  = errors.New("provider kind does not match the integration's expected provider kind")
	ErrProviderNotApplicable = errors.New("integration kind does not accept a provider reference")
)

// IntegrationKind describes a single installable integration.
// Implementations are registered in the global IntegrationRegistry
// at startup; they must be safe for concurrent reads (Validate is
// called from HTTP handlers).
type IntegrationKind interface {
	// Name is the stable slug identifying the kind across the API,
	// storage, and logs. Lowercase-alphanumeric + hyphens.
	Name() string
	// Description is operator-facing text rendered by the UI on the
	// "Available integrations" list. Markdown is not interpreted.
	Description() string
	// ProviderKind returns the Name() of the provider this
	// integration expects, or "" when the config is fully inline.
	ProviderKind() string
	// Validate parses the raw config blob and checks kind-specific
	// invariants (required fields, allowed values, referenced
	// provider consistency). Storage then writes the validated blob
	// verbatim — this is the only gate.
	Validate(config json.RawMessage, provider *storage.IntegrationProviderRecord) error
}

// ProviderKind describes a shared credential bundle. Separate from
// IntegrationKind because one provider (e.g. one Cloudflare account)
// can back many integrations across many groups.
type ProviderKind interface {
	Name() string
	Description() string
	Validate(config json.RawMessage) error
}

// IntegrationRegistry holds the known integration kinds.
type IntegrationRegistry struct {
	mu    sync.RWMutex
	kinds map[string]IntegrationKind
}

func NewIntegrationRegistry() *IntegrationRegistry {
	return &IntegrationRegistry{kinds: make(map[string]IntegrationKind)}
}

// Register installs a kind. Returns an error when a kind with the
// same Name is already present — registrations are expected to run
// once at boot.
func (r *IntegrationRegistry) Register(k IntegrationKind) error {
	if k == nil {
		return fmt.Errorf("integrations.Register: kind is nil")
	}
	name := k.Name()
	if name == "" {
		return fmt.Errorf("integrations.Register: kind has empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.kinds[name]; exists {
		return fmt.Errorf("integrations.Register: kind %q already registered", name)
	}
	r.kinds[name] = k
	return nil
}

// Get returns the kind with the given name. The second return is
// false when no such kind is registered.
func (r *IntegrationRegistry) Get(name string) (IntegrationKind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.kinds[name]
	return k, ok
}

// List returns every registered kind sorted by Name so the UI
// renders a stable catalog.
func (r *IntegrationRegistry) List() []IntegrationKind {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]IntegrationKind, 0, len(r.kinds))
	for _, k := range r.kinds {
		result = append(result, k)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name() < result[j].Name() })
	return result
}

// Validate resolves the kind by name and delegates to its Validate
// method. Returns ErrUnknownKind when the kind is not registered —
// HTTP handlers translate to 400.
func (r *IntegrationRegistry) Validate(kindName string, config json.RawMessage, provider *storage.IntegrationProviderRecord) error {
	kind, ok := r.Get(kindName)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownKind, kindName)
	}
	wantProvider := kind.ProviderKind()
	if wantProvider == "" && provider != nil {
		return fmt.Errorf("%w: kind %q", ErrProviderNotApplicable, kindName)
	}
	if wantProvider != "" && provider == nil {
		return fmt.Errorf("%w: kind %q expects provider kind %q", ErrProviderRequired, kindName, wantProvider)
	}
	if provider != nil && provider.Kind != wantProvider {
		return fmt.Errorf("%w: kind %q expects provider kind %q, got %q", ErrProviderKindMismatch, kindName, wantProvider, provider.Kind)
	}
	return kind.Validate(config, provider)
}

// ProviderRegistry holds the known shared-provider kinds. Mirrors
// IntegrationRegistry but for credential bundles.
type ProviderRegistry struct {
	mu    sync.RWMutex
	kinds map[string]ProviderKind
}

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{kinds: make(map[string]ProviderKind)}
}

func (r *ProviderRegistry) Register(k ProviderKind) error {
	if k == nil {
		return fmt.Errorf("integrations.ProviderRegistry.Register: kind is nil")
	}
	name := k.Name()
	if name == "" {
		return fmt.Errorf("integrations.ProviderRegistry.Register: kind has empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.kinds[name]; exists {
		return fmt.Errorf("integrations.ProviderRegistry.Register: kind %q already registered", name)
	}
	r.kinds[name] = k
	return nil
}

func (r *ProviderRegistry) Get(name string) (ProviderKind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.kinds[name]
	return k, ok
}

func (r *ProviderRegistry) List() []ProviderKind {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ProviderKind, 0, len(r.kinds))
	for _, k := range r.kinds {
		result = append(result, k)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name() < result[j].Name() })
	return result
}

// Validate resolves and validates a provider config payload.
func (r *ProviderRegistry) Validate(kindName string, config json.RawMessage) error {
	kind, ok := r.Get(kindName)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownProviderKind, kindName)
	}
	return kind.Validate(config)
}
