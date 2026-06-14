package sqlite

import (
	"context"
	"encoding/json"
	"errors"
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
	if !errors.Is(err, webhooks.ErrNotFound) {
		t.Errorf("MarkDelivered missing row err = %v, want webhooks.ErrNotFound", err)
	}
}

// TestPruneOutboxDeletesOnlyTerminalRows (C4): delivered-before-cutoff and
// dead-before-cutoff rows go away; pending and fresh-terminal rows stay.
func TestPruneOutboxDeletesOnlyTerminalRows(t *testing.T) {
	ws, cleanup := seedTestStore(t)
	defer cleanup()
	ctx := context.Background()
	insertEndpoint(t, ws, "ep-1", "https://example.com/hook", "secret", "", false, true)

	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-24 * time.Hour)
	old := now.Add(-48 * time.Hour)

	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := ws.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed outbox row: %v", err)
		}
	}
	// delivered long ago — pruned
	mustExec(`INSERT INTO webhook_outbox (id, endpoint_id, event_action, payload, next_attempt_at, created_at, delivered_at)
		VALUES ('row-delivered-old', 'ep-1', 'a.b', '{}', ?, ?, ?)`, old, old, old)
	// delivered recently — kept
	mustExec(`INSERT INTO webhook_outbox (id, endpoint_id, event_action, payload, next_attempt_at, created_at, delivered_at)
		VALUES ('row-delivered-new', 'ep-1', 'a.b', '{}', ?, ?, ?)`, now, now, now)
	// dead long ago — pruned
	mustExec(`INSERT INTO webhook_outbox (id, endpoint_id, event_action, payload, next_attempt_at, created_at, dead)
		VALUES ('row-dead-old', 'ep-1', 'a.b', '{}', ?, ?, 1)`, old, old)
	// pending, old — kept (never prune undelivered live rows)
	mustExec(`INSERT INTO webhook_outbox (id, endpoint_id, event_action, payload, next_attempt_at, created_at)
		VALUES ('row-pending-old', 'ep-1', 'a.b', '{}', ?, ?)`, old, old)

	pruned, err := ws.PruneOutbox(ctx, cutoff)
	if err != nil {
		t.Fatalf("PruneOutbox() error = %v", err)
	}
	if pruned != 2 {
		t.Fatalf("PruneOutbox() = %d, want 2", pruned)
	}

	var remaining int
	if err := ws.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_outbox`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("remaining rows = %d, want 2 (recent delivered + old pending)", remaining)
	}
}

func TestSQLiteWebhookStoreCRUD(t *testing.T) {
	ws, cleanup := seedTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	// Create.
	if err := ws.CreateEndpoint(ctx, webhooks.EndpointInput{
		ID: "ep-1", Name: "slack-prod", URL: "https://hooks.slack.com/x",
		SecretCiphertext: "PVS2:ciphered", EventFilter: "audit.*",
		AllowPrivate: false, Enabled: true,
	}, now); err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}

	// GetEndpointMeta — Secret must NOT be returned.
	meta, err := ws.GetEndpointMeta(ctx, "ep-1")
	if err != nil {
		t.Fatalf("GetEndpointMeta: %v", err)
	}
	if meta.Name != "slack-prod" || meta.URL != "https://hooks.slack.com/x" {
		t.Errorf("meta mismatch: %+v", meta)
	}
	if len(meta.Secret) != 0 {
		t.Errorf("Secret leaked through GetEndpointMeta: %q", meta.Secret)
	}
	if !meta.Enabled || meta.AllowPrivate {
		t.Errorf("flags not preserved: %+v", meta)
	}
	if got := meta.EventFilter; len(got) != 1 || got[0] != "audit.*" {
		t.Errorf("EventFilter = %v, want [audit.*]", got)
	}

	// Update without changing secret — existing ciphertext stays put.
	if err := ws.UpdateEndpoint(ctx, webhooks.EndpointInput{
		ID: "ep-1", Name: "slack-renamed", URL: "https://hooks.slack.com/y",
		SecretCiphertext: "", // empty = leave it
		EventFilter:      "audit.*,agent.*",
		AllowPrivate:     true, Enabled: false,
	}, now.Add(time.Minute)); err != nil {
		t.Fatalf("UpdateEndpoint(no-secret): %v", err)
	}
	// ListEnabledEndpoints excludes the now-disabled row.
	enabled, _ := ws.ListEnabledEndpoints(ctx)
	if len(enabled) != 0 {
		t.Errorf("ListEnabledEndpoints after disable should be empty, got %d", len(enabled))
	}
	// ListEndpointMeta is the admin view; includes disabled.
	all, _ := ws.ListEndpointMeta(ctx)
	if len(all) != 1 || all[0].Name != "slack-renamed" || all[0].Enabled {
		t.Errorf("ListEndpointMeta after rename+disable: %+v", all)
	}
	if got := all[0].EventFilter; len(got) != 2 || got[0] != "audit.*" || got[1] != "agent.*" {
		t.Errorf("EventFilter not updated: %v", got)
	}

	// Re-enable and re-verify worker can pick it up — confirms the
	// ciphertext on the row is still the original (the no-op
	// SecretCiphertext path didn't blank it).
	if err := ws.UpdateEndpoint(ctx, webhooks.EndpointInput{
		ID: "ep-1", Name: "slack-renamed", URL: "https://hooks.slack.com/y",
		SecretCiphertext: "", EventFilter: "audit.*",
		AllowPrivate: true, Enabled: true,
	}, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("UpdateEndpoint(re-enable): %v", err)
	}
	enabled, _ = ws.ListEnabledEndpoints(ctx)
	if len(enabled) != 1 {
		t.Fatalf("expected 1 enabled endpoint after re-enable, got %d", len(enabled))
	}
	if string(enabled[0].Secret) != "PVS2:ciphered" {
		t.Errorf("Secret lost across no-secret update: %q", enabled[0].Secret)
	}

	// Update WITH a new secret — rotates it.
	if err := ws.UpdateEndpoint(ctx, webhooks.EndpointInput{
		ID: "ep-1", Name: "slack-renamed", URL: "https://hooks.slack.com/y",
		SecretCiphertext: "PVS2:new",
		EventFilter:      "audit.*", AllowPrivate: true, Enabled: true,
	}, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("UpdateEndpoint(rotate-secret): %v", err)
	}
	enabled, _ = ws.ListEnabledEndpoints(ctx)
	if string(enabled[0].Secret) != "PVS2:new" {
		t.Errorf("Secret rotation failed: got %q want PVS2:new", enabled[0].Secret)
	}

	// Delete → not found.
	if err := ws.DeleteEndpoint(ctx, "ep-1"); err != nil {
		t.Fatalf("DeleteEndpoint: %v", err)
	}
	if _, err := ws.GetEndpointMeta(ctx, "ep-1"); !errors.Is(err, webhooks.ErrNotFound) {
		t.Errorf("GetEndpointMeta after delete err = %v, want ErrNotFound", err)
	}
	if err := ws.DeleteEndpoint(ctx, "ep-1"); !errors.Is(err, webhooks.ErrNotFound) {
		t.Errorf("DeleteEndpoint(missing) err = %v, want ErrNotFound", err)
	}
	if err := ws.UpdateEndpoint(ctx, webhooks.EndpointInput{ID: "ep-1", Name: "x", URL: "https://y", Enabled: true}, now); !errors.Is(err, webhooks.ErrNotFound) {
		t.Errorf("UpdateEndpoint(missing) err = %v, want ErrNotFound", err)
	}
}
