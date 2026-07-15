package telemt

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Contract-audit F7/M1: Telemt HAS a per-user enabled flag —
// CreateUserRequest.enabled (Option<bool>) and PatchUserRequest.enabled
// (Patch<bool>, mod.rs routes + users.rs handlers). The agent must ship it
// explicitly on both create and patch so disable/enable no longer maps to
// delete/create.

func enabledCaptureServer(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll() error = %v", err)
		}
		if err := json.Unmarshal(body, capture); err != nil {
			t.Errorf("json.Unmarshal(request) error = %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"links": map[string]any{"tls": []string{"tg://proxy?server=node-a&secret=x"}},
		})
	}))
}

func TestClientCreateClientSendsEnabledExplicitly(t *testing.T) {
	var requestBody map[string]any
	srv := enabledCaptureServer(t, &requestBody)
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.CreateClient(context.Background(), ManagedClient{
		Name:    "alice",
		Secret:  "00112233445566778899aabbccddeeff",
		Enabled: false,
	}); err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}

	got, present := requestBody["enabled"]
	if !present {
		t.Fatalf("create body has no enabled key; a disabled client would be created enabled (Telemt defaults to true)")
	}
	if got != false {
		t.Fatalf("create body enabled = %v, want false", got)
	}
}

func TestClientUpdateClientSendsEnabledExplicitly(t *testing.T) {
	var requestBody map[string]any
	srv := enabledCaptureServer(t, &requestBody)
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.UpdateClient(context.Background(), ManagedClient{
		Name:    "alice",
		Secret:  "00112233445566778899aabbccddeeff",
		Enabled: true,
	}); err != nil {
		t.Fatalf("UpdateClient() error = %v", err)
	}

	got, present := requestBody["enabled"]
	if !present {
		t.Fatalf("patch body has no enabled key; Telemt's Patch<bool> treats an omitted field as Unchanged, so a disable/enable would silently no-op")
	}
	if got != true {
		t.Fatalf("patch body enabled = %v, want true", got)
	}
}

// F7 residual: a real DELETE of the node's last configured user always 409s
// with code last_user_forbidden (telemt users.rs delete_user). Surface it
// typed so the panel shows the honest reason instead of a generic failure.
func TestClientDeleteClientLastUserForbiddenIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"last_user_forbidden","message":"Cannot delete the last configured user"}}`))
	}))
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	err = client.DeleteClient(context.Background(), "alice")
	if !errors.Is(err, ErrLastUserForbidden) {
		t.Fatalf("DeleteClient() on 409 last_user_forbidden error = %v, want errors.Is(_, ErrLastUserForbidden)", err)
	}
}

// TestClientDeleteClientOther409StaysGeneric keeps non-last_user 409s on the
// generic error path.
func TestClientDeleteClientOther409StaysGeneric(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"revision_conflict","message":"Config changed"}}`))
	}))
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Authorization: "secret"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	err = client.DeleteClient(context.Background(), "alice")
	if err == nil {
		t.Fatal("DeleteClient() error = nil, want failure")
	}
	if errors.Is(err, ErrLastUserForbidden) {
		t.Fatalf("DeleteClient() = ErrLastUserForbidden for a non-last_user 409 (err = %v)", err)
	}
}
