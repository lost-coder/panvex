package telemt

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Contract-audit F5: on modern Telemt the reset-quota route always exists,
// and a 404 there means the USER is absent from the node (telemt mod.rs:
// error code "not_found", message "User not found"). Only a route-level
// 404 ("Route not found") indicates a genuinely old Telemt (< 3.4.6)
// without the endpoint. The agent used to map EVERY 404 to
// ErrResetQuotaUnsupported, so operators saw "Telemt too old" for a
// simply-missing user.

func resetQuota404Server(t *testing.T, message string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"` + message + `"}}`))
	}))
}

func TestResetUserQuota404UserNotFoundIsNotUnsupported(t *testing.T) {
	srv := resetQuota404Server(t, "User not found")
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ResetUserQuota(context.Background(), "ghost")
	if err == nil {
		t.Fatal("ResetUserQuota() error = nil, want user-not-found error")
	}
	if errors.Is(err, ErrResetQuotaUnsupported) {
		t.Fatalf("ResetUserQuota() = ErrResetQuotaUnsupported for a missing USER; must not masquerade as old-Telemt (err = %v)", err)
	}
	if !errors.Is(err, ErrClientNotFound) {
		t.Fatalf("ResetUserQuota() error = %v, want errors.Is(_, ErrClientNotFound)", err)
	}
}

func TestResetUserQuota404RouteMissIsUnsupported(t *testing.T) {
	srv := resetQuota404Server(t, "Route not found")
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ResetUserQuota(context.Background(), "alice")
	if !errors.Is(err, ErrResetQuotaUnsupported) {
		t.Fatalf("ResetUserQuota() error = %v, want ErrResetQuotaUnsupported for a route-level 404 (old Telemt)", err)
	}
}
