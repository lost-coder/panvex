package telemt

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Contract-audit F6: Telemt answers 409 with error code "user_exists" when
// POST /v1/users targets an existing username (users.rs). The client job is
// a full desired-state upsert, so a redelivered create (panel retry after a
// lost ack) must be convergeable — the caller needs a typed sentinel to
// fall back to the PATCH path instead of failing forever.

func createConflictServer(t *testing.T, code, message string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}`))
	}))
}

func TestCreateClient409UserExistsIsTypedConflict(t *testing.T) {
	srv := createConflictServer(t, "user_exists", "User already exists")
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.CreateClient(context.Background(), ManagedClient{Name: "alice", Secret: "00112233445566778899aabbccddeeff"})
	if !errors.Is(err, ErrClientAlreadyExists) {
		t.Fatalf("CreateClient() on 409 user_exists error = %v, want errors.Is(_, ErrClientAlreadyExists)", err)
	}
}

func TestCreateClient409OtherCodeStaysGeneric(t *testing.T) {
	srv := createConflictServer(t, "last_user_forbidden", "Cannot remove the last user")
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.CreateClient(context.Background(), ManagedClient{Name: "alice", Secret: "00112233445566778899aabbccddeeff"})
	if err == nil {
		t.Fatal("CreateClient() error = nil, want failure")
	}
	if errors.Is(err, ErrClientAlreadyExists) {
		t.Fatalf("CreateClient() = ErrClientAlreadyExists for a non-user_exists 409 (err = %v)", err)
	}
}
