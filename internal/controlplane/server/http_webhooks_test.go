package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/controlplane/webhooks"
)

// testWebhookServer wires a Server with WebhookStorageFactory + a
// bootstrapped admin user and returns admin login cookies. Same
// pattern as http_retention_test's setup, extended to cover the
// webhook subsystem.
func testWebhookServer(t *testing.T, now time.Time) (*Server, []*http.Cookie) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "panvex.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
		WebhookStorageFactory: func(decrypt webhooks.SecretDecrypter) webhooks.Storage {
			return sqlite.NewWebhookStore(store.DB(), decrypt)
		},
	})
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}
	login := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body = %s", login.Code, login.Body.String())
	}
	return server, login.Result().Cookies()
}

func TestWebhookEndpointCRUDFlow(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	server, cookies := testWebhookServer(t, now)

	// Empty list on a fresh deployment.
	listResp := performJSONRequest(t, server, http.MethodGet, "/api/webhook-endpoints", nil, cookies)
	if listResp.Code != http.StatusOK {
		t.Fatalf("GET /api/webhook-endpoints (empty) status = %d, body = %s", listResp.Code, listResp.Body.String())
	}
	var listBody struct {
		Endpoints []webhookEndpointDTO `json:"endpoints"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(listBody.Endpoints) != 0 {
		t.Fatalf("expected 0 endpoints, got %d", len(listBody.Endpoints))
	}

	// Create.
	createResp := performJSONRequest(t, server, http.MethodPost, "/api/webhook-endpoints", map[string]any{
		"name":          "slack-prod",
		"url":           "https://hooks.slack.com/x",
		"secret":        "supersecretkey-01",
		"event_filter":  "audit.*",
		"allow_private": false,
		"enabled":       true,
	}, cookies)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("POST /api/webhook-endpoints status = %d; body = %s", createResp.Code, createResp.Body.String())
	}
	var created webhookEndpointDTO
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	if created.ID == "" || created.Name != "slack-prod" {
		t.Errorf("created DTO bad: %+v", created)
	}

	// GET single.
	getResp := performJSONRequest(t, server, http.MethodGet, "/api/webhook-endpoints/"+created.ID, nil, cookies)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET single status = %d", getResp.Code)
	}
	var got webhookEndpointDTO
	_ = json.Unmarshal(getResp.Body.Bytes(), &got)
	if got != created {
		t.Errorf("GET round-trip mismatch: got %+v want %+v", got, created)
	}

	// Update without rotating secret.
	updResp := performJSONRequest(t, server, http.MethodPut, "/api/webhook-endpoints/"+created.ID, map[string]any{
		"name":          "slack-renamed",
		"url":           "https://hooks.slack.com/y",
		"secret":        "", // leave existing secret in place
		"event_filter":  "audit.*,agent.*",
		"allow_private": false,
		"enabled":       true,
	}, cookies)
	if updResp.Code != http.StatusOK {
		t.Fatalf("PUT status = %d; body = %s", updResp.Code, updResp.Body.String())
	}

	// Worker-side ListEnabledEndpoints must still return the secret
	// (via the vault decrypter) — confirms an empty-secret update
	// did NOT blank it.
	enabled, err := server.webhookStorage.ListEnabledEndpoints(context.Background())
	if err != nil {
		t.Fatalf("ListEnabledEndpoints: %v", err)
	}
	if len(enabled) != 1 {
		t.Fatalf("ListEnabledEndpoints = %d, want 1", len(enabled))
	}
	// Secret was vault-encrypted on create (the vault is a no-op
	// pass-through when EncryptionKey is empty in test, so the
	// plaintext should round-trip unchanged).
	if string(enabled[0].Secret) != "supersecretkey-01" {
		t.Errorf("Secret lost across no-rotate update: %q", enabled[0].Secret)
	}

	// Update with rotated secret.
	rotResp := performJSONRequest(t, server, http.MethodPut, "/api/webhook-endpoints/"+created.ID, map[string]any{
		"name":          "slack-renamed",
		"url":           "https://hooks.slack.com/y",
		"secret":        "rotated-key-12345",
		"event_filter":  "audit.*",
		"allow_private": false,
		"enabled":       true,
	}, cookies)
	if rotResp.Code != http.StatusOK {
		t.Fatalf("PUT(rotate) status = %d", rotResp.Code)
	}
	enabled, _ = server.webhookStorage.ListEnabledEndpoints(context.Background())
	if string(enabled[0].Secret) != "rotated-key-12345" {
		t.Errorf("Secret rotation failed: %q", enabled[0].Secret)
	}

	// Delete.
	delResp := performJSONRequest(t, server, http.MethodDelete, "/api/webhook-endpoints/"+created.ID, nil, cookies)
	if delResp.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, body = %s", delResp.Code, delResp.Body.String())
	}
	getAfter := performJSONRequest(t, server, http.MethodGet, "/api/webhook-endpoints/"+created.ID, nil, cookies)
	if getAfter.Code != http.StatusNotFound {
		t.Errorf("GET after delete status = %d, want 404", getAfter.Code)
	}
}

func TestWebhookEndpointValidation(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	server, cookies := testWebhookServer(t, now)

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{
			"empty name",
			map[string]any{"url": "https://x.example.com", "secret": "s"},
			http.StatusBadRequest,
		},
		{
			"missing secret",
			map[string]any{"name": "x", "url": "https://x.example.com"},
			http.StatusBadRequest,
		},
		{
			"non-http scheme",
			map[string]any{"name": "x", "url": "ftp://x.example.com", "secret": "s"},
			http.StatusBadRequest,
		},
		{
			"invalid event_filter syntax",
			map[string]any{"name": "x", "url": "https://x.example.com", "secret": "s", "event_filter": "agent[*"},
			http.StatusBadRequest,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := performJSONRequest(t, server, http.MethodPost, "/api/webhook-endpoints", c.body, cookies)
			if resp.Code != c.want {
				t.Errorf("status = %d, want %d; body = %s", resp.Code, c.want, resp.Body.String())
			}
		})
	}
}

func TestWebhookEndpointAdminGated(t *testing.T) {
	// Operator role must NOT be able to manage webhook endpoints —
	// these are config secrets, not operational levers.
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	server, _ := testWebhookServer(t, now)

	// Bootstrap an operator user and log in.
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "op",
		Password: "Op1password",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(op): %v", err)
	}
	opLogin := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "op",
		"password": "Op1password",
	}, nil)
	if opLogin.Code != http.StatusOK {
		t.Fatalf("op login status = %d", opLogin.Code)
	}
	opCookies := opLogin.Result().Cookies()

	for _, route := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/webhook-endpoints", nil},
		{http.MethodGet, "/api/webhook-endpoints/x", nil},
		{http.MethodPost, "/api/webhook-endpoints", map[string]any{"name": "x", "url": "https://y", "secret": "z"}},
		{http.MethodPut, "/api/webhook-endpoints/x", map[string]any{"name": "x", "url": "https://y"}},
		{http.MethodDelete, "/api/webhook-endpoints/x", nil},
	} {
		t.Run(fmt.Sprintf("%s %s", route.method, route.path), func(t *testing.T) {
			resp := performJSONRequest(t, server, route.method, route.path, route.body, opCookies)
			if resp.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403; body = %s", resp.Code, resp.Body.String())
			}
		})
	}
}

func TestWebhookEndpointAuditFlow(t *testing.T) {
	// Each Create/Update/Delete must land an audit row that the
	// existing audit-trail consumers (and the webhook fan-out
	// itself, eventually) can observe.
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	server, cookies := testWebhookServer(t, now)

	createResp := performJSONRequest(t, server, http.MethodPost, "/api/webhook-endpoints", map[string]any{
		"name":    "ep1",
		"url":     "https://x.example.com",
		"secret":  "audit-flow-secret",
		"enabled": true,
	}, cookies)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create: %d; %s", createResp.Code, createResp.Body.String())
	}

	// Audit ring buffer is in-memory; read it back.
	server.metricsAuditMu.RLock()
	events := server.snapshotAuditTrailLocked()
	server.metricsAuditMu.RUnlock()
	found := false
	for _, ev := range events {
		if ev.Action == "webhook.endpoint.create" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("audit log missing webhook.endpoint.create; got actions = %v", auditActions(events))
	}
}

// auditActions extracts the Action of each event for diagnostic output.
func auditActions(events []AuditEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Action)
	}
	return out
}

// TestValidateWebhookFormRejectsShortSecret asserts that Create-path
// validation rejects a 1-character secret. HMAC-SHA-256 with a key
// that short is trivially forgeable; see C-2 in the 2026-05-12 review.
func TestValidateWebhookFormRejectsShortSecret(t *testing.T) {
	err := validateWebhookForm("ok-name", "https://example.com/", "x", "", true)
	if err == nil {
		t.Fatal("expected error for 1-char secret")
	}
	if !strings.Contains(err.Error(), "secret too short") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestValidateWebhookFormAcceptsMinLengthSecret asserts that a secret
// at exactly the 16-byte minimum is accepted on the Create path.
func TestValidateWebhookFormAcceptsMinLengthSecret(t *testing.T) {
	err := validateWebhookForm("ok-name", "https://example.com/", strings.Repeat("a", 16), "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestValidateWebhookFormUpdateWithShortSecretRejected asserts that a
// non-empty short secret on the Update path is still rejected — Update
// with a non-empty secret persists it, so the min-length floor must
// apply there too.
func TestValidateWebhookFormUpdateWithShortSecretRejected(t *testing.T) {
	err := validateWebhookForm("ok-name", "https://example.com/", "short", "", false)
	if err == nil {
		t.Fatal("expected error for short secret on Update path")
	}
	if !strings.Contains(err.Error(), "secret too short") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestValidateWebhookFormUpdateWithEmptySecretAccepted asserts that
// keep-existing semantics (empty secret on Update) still work. The
// min-length check must not regress this case.
func TestValidateWebhookFormUpdateWithEmptySecretAccepted(t *testing.T) {
	err := validateWebhookForm("ok-name", "https://example.com/", "", "", false)
	if err != nil {
		t.Fatalf("unexpected error for empty secret on Update: %v", err)
	}
}

// TestValidateWebhookFormRejectsWhitespacePaddedSecret asserts that a
// 16-byte string with only one non-whitespace byte (e.g. "a" + 15
// spaces) is rejected. Raw byte length passes the old check but the
// HMAC entropy is effectively 1 byte.
func TestValidateWebhookFormRejectsWhitespacePaddedSecret(t *testing.T) {
	padded := "a" + strings.Repeat(" ", 15)
	if len(padded) != 16 {
		t.Fatalf("test fixture wrong length: %d", len(padded))
	}
	err := validateWebhookForm("ok-name", "https://example.com/", padded, "", true)
	if err == nil || !strings.Contains(err.Error(), "secret too short") {
		t.Fatalf("expected 'secret too short' error, got %v", err)
	}
}

// TestValidateWebhookFormRejectsAllWhitespaceOnUpdate asserts that a
// non-empty all-whitespace secret on the Update path is rejected
// rather than persisted as a zero-entropy HMAC key. The handler keys
// rotation off req.Secret != "", so 16 spaces would otherwise sneak
// past as a "rotation" to an all-whitespace key.
func TestValidateWebhookFormRejectsAllWhitespaceOnUpdate(t *testing.T) {
	allWs := strings.Repeat(" ", 16)
	err := validateWebhookForm("ok-name", "https://example.com/", allWs, "", false)
	if err == nil || !strings.Contains(err.Error(), "secret too short") {
		t.Fatalf("expected 'secret too short' on Update with whitespace-only secret, got %v", err)
	}
}
