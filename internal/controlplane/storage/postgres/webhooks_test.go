package postgres

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPruneOutboxDeletesOnlyTerminalRowsPostgres(t *testing.T) {
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.sqlDB.ExecContext(ctx, `TRUNCATE TABLE webhook_outbox, webhook_endpoints CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	ws := NewWebhookStore(store.sqlDB, nil)
	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-24 * time.Hour)
	old := now.Add(-48 * time.Hour)

	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := store.sqlDB.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO webhook_endpoints (id, name, url, secret_ciphertext) VALUES ('ep-1', 'n', 'https://example.com', 's')`)
	mustExec(`INSERT INTO webhook_outbox (id, endpoint_id, event_action, payload, next_attempt_at, created_at, delivered_at)
		VALUES ('row-delivered-old', 'ep-1', 'a.b', '{}'::jsonb, $1, $1, $1)`, old)
	mustExec(`INSERT INTO webhook_outbox (id, endpoint_id, event_action, payload, next_attempt_at, created_at, delivered_at)
		VALUES ('row-delivered-new', 'ep-1', 'a.b', '{}'::jsonb, $1, $1, $1)`, now)
	mustExec(`INSERT INTO webhook_outbox (id, endpoint_id, event_action, payload, next_attempt_at, created_at, dead)
		VALUES ('row-dead-old', 'ep-1', 'a.b', '{}'::jsonb, $1, $1, TRUE)`, old)
	mustExec(`INSERT INTO webhook_outbox (id, endpoint_id, event_action, payload, next_attempt_at, created_at)
		VALUES ('row-pending-old', 'ep-1', 'a.b', '{}'::jsonb, $1, $1)`, old)

	pruned, err := ws.PruneOutbox(ctx, cutoff)
	if err != nil {
		t.Fatalf("PruneOutbox() error = %v", err)
	}
	if pruned != 2 {
		t.Fatalf("PruneOutbox() = %d, want 2", pruned)
	}
}
