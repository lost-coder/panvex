package telemt

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestResetUserQuotaSuccessParsesSnapshot verifies that a 200 response
// from POST /v1/users/{u}/reset-quota is decoded into the typed
// ResetUserQuotaResult. The panel relies on the post-reset
// last_reset_epoch_secs to drive immediate UI updates ("Last reset:
// just now") without waiting for the next /v1/users/quota poll.
func TestResetUserQuotaSuccessParsesSnapshot(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"username":"alice","used_bytes":0,"last_reset_epoch_secs":1747332000}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Authorization: "secret"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	snapshot, err := client.ResetUserQuota(context.Background(), "alice")
	if err != nil {
		t.Fatalf("ResetUserQuota() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("HTTP method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/users/alice/reset-quota" {
		t.Fatalf("request path = %q, want %q", gotPath, "/v1/users/alice/reset-quota")
	}
	if snapshot.Username != "alice" || snapshot.LastResetEpochSecs != 1747332000 {
		t.Fatalf("snapshot = %+v, want {alice, 0, 1747332000}", snapshot)
	}
}

// TestResetUserQuotaReturnsUnsupportedOn404 anchors the version-tolerance
// contract: a Telemt that predates 3.4.6 has no /reset-quota route, so it
// answers 404 and we surface ErrResetQuotaUnsupported. The agent's job
// handler matches this typed error and tells the panel to render "Reset
// unavailable (Telemt < 3.4.6)" instead of a generic transport failure.
func TestResetUserQuotaReturnsUnsupportedOn404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Route not found"}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Authorization: "secret"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ResetUserQuota(context.Background(), "alice")
	if !errors.Is(err, ErrResetQuotaUnsupported) {
		t.Fatalf("error = %v, want errors.Is(ErrResetQuotaUnsupported)", err)
	}
}

// TestResetUserQuotaReturnsReadOnlyOn403 is the read-only twin: a 403
// from Telemt (API in read-only mode) surfaces as ErrResetQuotaReadOnly
// so the UI can suggest the operator lift read-only rather than retry.
func TestResetUserQuotaReturnsReadOnlyOn403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"read_only","message":"API runs in read-only mode"}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Authorization: "secret"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ResetUserQuota(context.Background(), "alice")
	if !errors.Is(err, ErrResetQuotaReadOnly) {
		t.Fatalf("error = %v, want errors.Is(ErrResetQuotaReadOnly)", err)
	}
}

// TestResetUserQuotaOnTransportFailureReturnsGenericError keeps the
// failure-classifier honest: a 500 must NOT collapse into one of the
// typed sentinel errors, otherwise the panel would tell the operator
// "Telemt < 3.4.6" while the real issue is a Telemt-side bug or load.
func TestResetUserQuotaOnTransportFailureReturnsGenericError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal","message":"boom"}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Authorization: "secret"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ResetUserQuota(context.Background(), "alice")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrResetQuotaUnsupported) || errors.Is(err, ErrResetQuotaReadOnly) {
		t.Fatalf("transport failure must not classify as a sentinel; got %v", err)
	}
}
