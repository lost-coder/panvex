package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/webhooks"
)

// seedTestStore opens a fresh SQLite store and returns a webhook
// storage backend wired with an identity decrypter (plaintext
// secrets — production wires a vault-backed decrypter via
// server.New).
func seedTestStore(t *testing.T) (*WebhookStore, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "panvex.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	cleanup := func() { _ = store.Close() }
	return NewWebhookStore(store.sqlDB, nil), cleanup
}

func insertEndpoint(t *testing.T, ws *WebhookStore, id, url, secret, filter string, allowPrivate, enabled bool) {
	t.Helper()
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	allowInt := 0
	if allowPrivate {
		allowInt = 1
	}
	if _, err := ws.db.ExecContext(context.Background(), `
		INSERT INTO webhook_endpoints
			(id, name, url, secret_ciphertext, event_filter, allow_private, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, id, url, secret, filter, allowInt, enabledInt); err != nil {
		t.Fatalf("insert endpoint %s: %v", id, err)
	}
}

func TestSQLiteWebhookStoreListEnabled(t *testing.T) {
	ws, cleanup := seedTestStore(t)
	defer cleanup()
	ctx := context.Background()

	insertEndpoint(t, ws, "ep-1", "https://a.example.com/h", "k1", "agent.*", true, true)
	insertEndpoint(t, ws, "ep-2", "https://b.example.com/h", "k2", "", false, true)
	insertEndpoint(t, ws, "ep-3", "https://c.example.com/h", "k3", "agent.*", false, false) // disabled

	eps, err := ws.ListEnabledEndpoints(ctx)
	if err != nil {
		t.Fatalf("ListEnabledEndpoints: %v", err)
	}
	if got := len(eps); got != 2 {
		t.Fatalf("len = %d, want 2 (disabled excluded)", got)
	}
	byID := map[string]webhooks.Endpoint{}
	for _, ep := range eps {
		byID[ep.ID] = ep
	}
	if string(byID["ep-1"].Secret) != "k1" {
		t.Errorf("ep-1 Secret = %q, want plaintext k1 (identity decrypter)", byID["ep-1"].Secret)
	}
	if !byID["ep-1"].AllowPrivate {
		t.Errorf("ep-1 AllowPrivate not preserved")
	}
	if got := byID["ep-1"].EventFilter; len(got) != 1 || got[0] != "agent.*" {
		t.Errorf("ep-1 EventFilter = %v, want [agent.*]", got)
	}
	if len(byID["ep-2"].EventFilter) != 0 {
		t.Errorf("ep-2 EventFilter should be empty (broadcast), got %v", byID["ep-2"].EventFilter)
	}
}

func TestSQLiteWebhookStoreOutboxRoundTrip(t *testing.T) {
	ws, cleanup := seedTestStore(t)
	defer cleanup()
	ctx := context.Background()

	insertEndpoint(t, ws, "ep-1", "https://a.example.com", "k", "", true, true)

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	row := webhooks.OutboxRow{
		ID:            "row-1",
		EndpointID:    "ep-1",
		EventAction:   "agent.unhealthy",
		Payload:       json.RawMessage(`{"agent":"a-1"}`),
		NextAttemptAt: now,
		CreatedAt:     now,
	}
	if err := ws.InsertOutbox(ctx, row); err != nil {
		t.Fatalf("InsertOutbox: %v", err)
	}

	// Claim — round-trips through both endpoint and outbox columns.
	deliveries, err := ws.ClaimReady(ctx, now, 16)
	if err != nil {
		t.Fatalf("ClaimReady: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("len(deliveries) = %d, want 1", len(deliveries))
	}
	d := deliveries[0]
	if d.Outbox.ID != row.ID || d.Outbox.EventAction != row.EventAction {
		t.Errorf("outbox round-trip mismatch: got %+v", d.Outbox)
	}
	if string(d.Outbox.Payload) != string(row.Payload) {
		t.Errorf("payload mismatch: got %s want %s", d.Outbox.Payload, row.Payload)
	}
	if !d.Outbox.NextAttemptAt.Equal(now) {
		t.Errorf("NextAttemptAt = %v, want %v", d.Outbox.NextAttemptAt, now)
	}
	if d.Endpoint.ID != "ep-1" || string(d.Endpoint.Secret) != "k" {
		t.Errorf("endpoint join failed: %+v", d.Endpoint)
	}
}

func TestSQLiteWebhookStoreMarkPaths(t *testing.T) {
	ws, cleanup := seedTestStore(t)
	defer cleanup()
	ctx := context.Background()

	insertEndpoint(t, ws, "ep-1", "https://a.example.com", "k", "", true, true)
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	if err := ws.InsertOutbox(ctx, webhooks.OutboxRow{
		ID: "ok", EndpointID: "ep-1", EventAction: "x.y",
		Payload: json.RawMessage(`{}`), NextAttemptAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ws.InsertOutbox(ctx, webhooks.OutboxRow{
		ID: "fail", EndpointID: "ep-1", EventAction: "x.y",
		Payload: json.RawMessage(`{}`), NextAttemptAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := ws.MarkDelivered(ctx, "ok", now.Add(time.Second)); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if err := ws.MarkFailed(ctx, "fail", 3, now.Add(time.Minute), "boom", true); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	// After both marks, a fresh ClaimReady at the same instant must
	// yield zero rows: ok is delivered, fail is dead.
	deliveries, err := ws.ClaimReady(ctx, now.Add(time.Hour), 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 0 {
		t.Errorf("ClaimReady should return 0 rows after MarkDelivered + MarkFailed(dead); got %d", len(deliveries))
	}

	// Marking a non-existent ID must surface ErrNotFound (so the
	// worker logs without crashing).
	err = ws.MarkDelivered(ctx, "no-such-row", now)
	if err != webhooks.ErrNotFound {
		t.Errorf("MarkDelivered missing row err = %v, want webhooks.ErrNotFound", err)
	}
}
