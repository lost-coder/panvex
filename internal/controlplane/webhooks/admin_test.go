package webhooks

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeAdminStore records the last Create/Update input so tests can assert what
// the AdminService persisted.
type fakeAdminStore struct {
	created   *EndpointInput
	updated   *EndpointInput
	createdAt time.Time
	updatedAt time.Time
	getErr    error
	deleted   string
}

func (f *fakeAdminStore) ListEndpointMeta(context.Context) ([]Endpoint, error) { return nil, nil }
func (f *fakeAdminStore) GetEndpointMeta(_ context.Context, _ string) (Endpoint, error) {
	return Endpoint{}, f.getErr
}
func (f *fakeAdminStore) CreateEndpoint(_ context.Context, in EndpointInput, now time.Time) error {
	f.created = &in
	f.createdAt = now
	return nil
}
func (f *fakeAdminStore) UpdateEndpoint(_ context.Context, in EndpointInput, now time.Time) error {
	f.updated = &in
	f.updatedAt = now
	return nil
}
func (f *fakeAdminStore) DeleteEndpoint(_ context.Context, id string) error {
	f.deleted = id
	return nil
}

func encPrefix(plaintext string) (string, error) { return "enc:" + plaintext, nil }

func TestAdminCreateEncryptsSecretAndStampsNow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 3, 8, 0, 0, 0, time.UTC)
	store := &fakeAdminStore{}
	svc := NewAdminService(store, encPrefix, func() time.Time { return now })

	err := svc.Create(context.Background(), EndpointForm{ID: "w1", Name: "n", URL: "https://x", Secret: "sekret", Enabled: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if store.created == nil {
		t.Fatal("nothing persisted")
	}
	if store.created.SecretCiphertext != "enc:sekret" {
		t.Fatalf("secret not encrypted before persist: %q", store.created.SecretCiphertext)
	}
	if store.created.ID != "w1" || !store.created.Enabled {
		t.Fatalf("form fields not carried: %#v", *store.created)
	}
	if !store.createdAt.Equal(now.UTC()) {
		t.Fatalf("createdAt = %v, want %v", store.createdAt, now.UTC())
	}
}

func TestAdminUpdateWithSecretRotates(t *testing.T) {
	t.Parallel()
	store := &fakeAdminStore{}
	svc := NewAdminService(store, encPrefix, time.Now)
	if err := svc.Update(context.Background(), EndpointForm{ID: "w1", Secret: "new"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if store.updated == nil || store.updated.SecretCiphertext != "enc:new" {
		t.Fatalf("update did not rotate secret: %#v", store.updated)
	}
}

func TestAdminUpdateWithEmptySecretKeepsExisting(t *testing.T) {
	t.Parallel()
	store := &fakeAdminStore{}
	svc := NewAdminService(store, encPrefix, time.Now)
	if err := svc.Update(context.Background(), EndpointForm{ID: "w1", Secret: ""}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if store.updated == nil {
		t.Fatal("nothing persisted")
	}
	if store.updated.SecretCiphertext != "" {
		t.Fatalf("empty secret must leave SecretCiphertext blank (keep existing), got %q", store.updated.SecretCiphertext)
	}
}

func TestAdminGetPropagatesNotFound(t *testing.T) {
	t.Parallel()
	store := &fakeAdminStore{getErr: ErrNotFound}
	svc := NewAdminService(store, encPrefix, time.Now)
	if _, err := svc.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get should propagate ErrNotFound, got %v", err)
	}
}

func TestAdminDeleteDelegates(t *testing.T) {
	t.Parallel()
	store := &fakeAdminStore{}
	svc := NewAdminService(store, encPrefix, time.Now)
	if err := svc.Delete(context.Background(), "w9"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if store.deleted != "w9" {
		t.Fatalf("Delete not delegated, got %q", store.deleted)
	}
}
