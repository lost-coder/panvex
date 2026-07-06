package webhooks

import (
	"context"
	"time"
)

// SecretCipher encrypts an endpoint's plaintext HMAC secret before it is
// persisted. The server supplies a closure over its secret vault so the
// plaintext never reaches storage code.
type SecretCipher func(plaintext string) (string, error)

// AdminStore is the operator-CRUD subset of Storage the AdminService needs.
// storage.Store (and the in-memory test fixture) satisfy it structurally.
type AdminStore interface {
	ListEndpointMeta(ctx context.Context) ([]Endpoint, error)
	GetEndpointMeta(ctx context.Context, id string) (Endpoint, error)
	CreateEndpoint(ctx context.Context, in EndpointInput, now time.Time) error
	UpdateEndpoint(ctx context.Context, in EndpointInput, now time.Time) error
	DeleteEndpoint(ctx context.Context, id string) error
}

// EndpointForm is the operator-supplied endpoint definition with a PLAINTEXT
// secret. AdminService encrypts Secret via its SecretCipher before persisting.
// An empty Secret on Update leaves the existing secret unchanged.
type EndpointForm struct {
	ID           string
	Name         string
	URL          string
	Secret       string
	EventFilter  string
	AllowPrivate bool
	Enabled      bool
}

// AdminService owns the operator webhook-endpoint CRUD: it centralises the
// "encrypt the secret before it hits storage" invariant so no handler can
// forget it. Read handlers keep request decoding, validation, and the DTO
// mapping; this service owns the cipher + store round-trips + the clock.
type AdminService struct {
	store   AdminStore
	encrypt SecretCipher
	now     func() time.Time
}

// NewAdminService constructs an AdminService. encrypt is the vault-backed
// cipher closure; now supplies the created/updated timestamp.
func NewAdminService(store AdminStore, encrypt SecretCipher, now func() time.Time) *AdminService {
	return &AdminService{store: store, encrypt: encrypt, now: now}
}

// List returns every endpoint (including disabled) with secrets elided.
func (a *AdminService) List(ctx context.Context) ([]Endpoint, error) {
	return a.store.ListEndpointMeta(ctx)
}

// Get returns one endpoint's operator-visible fields (secret elided). A
// missing endpoint surfaces the store's ErrNotFound to the caller.
func (a *AdminService) Get(ctx context.Context, id string) (Endpoint, error) {
	return a.store.GetEndpointMeta(ctx, id)
}

// Create encrypts the form's plaintext secret and persists a new endpoint.
func (a *AdminService) Create(ctx context.Context, form EndpointForm) error {
	ciphertext, err := a.encrypt(form.Secret)
	if err != nil {
		return err
	}
	return a.store.CreateEndpoint(ctx, inputFrom(form, ciphertext), a.now().UTC())
}

// Update persists an endpoint. A non-empty Secret is re-encrypted and rotates
// the stored secret; an empty Secret leaves the existing secret in place
// (SecretCiphertext == "" is the store's "keep existing" signal). A missing
// endpoint surfaces the store's ErrNotFound.
func (a *AdminService) Update(ctx context.Context, form EndpointForm) error {
	var ciphertext string
	if form.Secret != "" {
		ct, err := a.encrypt(form.Secret)
		if err != nil {
			return err
		}
		ciphertext = ct
	}
	return a.store.UpdateEndpoint(ctx, inputFrom(form, ciphertext), a.now().UTC())
}

// Delete removes an endpoint. A missing endpoint surfaces the store's ErrNotFound.
func (a *AdminService) Delete(ctx context.Context, id string) error {
	return a.store.DeleteEndpoint(ctx, id)
}

func inputFrom(form EndpointForm, ciphertext string) EndpointInput {
	return EndpointInput{
		ID:               form.ID,
		Name:             form.Name,
		URL:              form.URL,
		SecretCiphertext: ciphertext,
		EventFilter:      form.EventFilter,
		AllowPrivate:     form.AllowPrivate,
		Enabled:          form.Enabled,
	}
}
