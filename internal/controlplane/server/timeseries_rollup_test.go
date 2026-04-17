package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestRollupWorkerPrunesAuditAndMetrics exercises a single runTimeseriesRollup
// tick against a real SQLite-backed store and verifies:
//   - audit_events rows older than the retention cutoff are deleted (P2-REL-04)
//   - metric_snapshots rows older than the retention cutoff are deleted
//     (P2-REL-05)
//   - the panvex_retention_pruned_rows_total counter increments with the
//     correct "table" label
func TestRollupWorkerPrunesAuditAndMetrics(t *testing.T) {
	now := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })

	server := New(Options{
		Now:                func() time.Time { return now },
		Store:              sqliteStore,
		MetricsScrapeToken: "devtoken",
	})
	t.Cleanup(server.Close)

	ctx := context.Background()

	// Configure retention so "old" rows (> 24h) get pruned. Defaults (90d for
	// audit, 30d for metrics) would be too lenient for the fixture timestamps.
	settings := RetentionSettings{
		TSRawSeconds:          86400,
		TSHourlySeconds:       604800,
		TSDCSeconds:           86400,
		IPHistorySeconds:      2592000,
		EventSeconds:          86400,
		AuditEventSeconds:     24 * 3600,
		MetricSnapshotSeconds: 24 * 3600,
	}
	server.settingsMu.Lock()
	server.retention = settings
	server.settingsMu.Unlock()

	seedAudit := []storage.AuditEventRecord{
		{ID: "a-old-1", ActorID: "u", Action: "auth.login", TargetID: "t", CreatedAt: now.Add(-72 * time.Hour), Details: map[string]any{"old": "1"}},
		{ID: "a-old-2", ActorID: "u", Action: "auth.login", TargetID: "t", CreatedAt: now.Add(-48 * time.Hour), Details: map[string]any{"old": "2"}},
		{ID: "a-keep", ActorID: "u", Action: "auth.login", TargetID: "t", CreatedAt: now.Add(-1 * time.Hour), Details: map[string]any{"keep": "1"}},
	}
	for _, e := range seedAudit {
		if err := sqliteStore.AppendAuditEvent(ctx, e); err != nil {
			t.Fatalf("AppendAuditEvent(%s) error = %v", e.ID, err)
		}
	}

	seedMetrics := []storage.MetricSnapshotRecord{
		{ID: "m-old-1", AgentID: "a1", InstanceID: "i1", CapturedAt: now.Add(-72 * time.Hour), Values: map[string]uint64{"v": 1}},
		{ID: "m-old-2", AgentID: "a1", InstanceID: "i1", CapturedAt: now.Add(-48 * time.Hour), Values: map[string]uint64{"v": 2}},
		{ID: "m-keep", AgentID: "a1", InstanceID: "i1", CapturedAt: now.Add(-1 * time.Hour), Values: map[string]uint64{"v": 3}},
	}
	for _, m := range seedMetrics {
		if err := sqliteStore.AppendMetricSnapshot(ctx, m); err != nil {
			t.Fatalf("AppendMetricSnapshot(%s) error = %v", m.ID, err)
		}
	}

	server.runTimeseriesRollup(ctx)

	events, err := sqliteStore.ListAuditEvents(ctx, 0)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(ListAuditEvents()) after rollup = %d, want 1", len(events))
	}
	if events[0].ID != "a-keep" {
		t.Fatalf("retained audit event ID = %q, want %q", events[0].ID, "a-keep")
	}

	snapshots, err := sqliteStore.ListMetricSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListMetricSnapshots() error = %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("len(ListMetricSnapshots()) after rollup = %d, want 1", len(snapshots))
	}
	if snapshots[0].ID != "m-keep" {
		t.Fatalf("retained metric snapshot ID = %q, want %q", snapshots[0].ID, "m-keep")
	}

	// Assert the Prometheus counter reflects the deletions.
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer devtoken")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	text := string(body)
	for _, want := range []string{
		`panvex_retention_pruned_rows_total{table="audit_events"} 2`,
		`panvex_retention_pruned_rows_total{table="metric_snapshots"} 2`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing expected metric line %q in:\n%s", want, text)
		}
	}
}

// TestRollupWorkerSkipsDisabledRetention verifies that when an operator sets
// AuditEventSeconds or MetricSnapshotSeconds to zero (the "disabled" marker),
// the worker does not call the corresponding prune method and the counter
// stays at its pre-initialised zero value.
func TestRollupWorkerSkipsDisabledRetention(t *testing.T) {
	now := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: sqliteStore,
	})
	t.Cleanup(server.Close)

	// Explicitly disable audit + metric retention while keeping the other
	// retention knobs populated.
	settings := defaultRetentionSettings()
	settings.AuditEventSeconds = 0
	settings.MetricSnapshotSeconds = 0
	server.settingsMu.Lock()
	server.retention = settings
	server.settingsMu.Unlock()

	ctx := context.Background()

	old := now.Add(-1000 * time.Hour)
	if err := sqliteStore.AppendAuditEvent(ctx, storage.AuditEventRecord{
		ID: "a1", ActorID: "u", Action: "x", TargetID: "t", CreatedAt: old,
		Details: map[string]any{},
	}); err != nil {
		t.Fatalf("AppendAuditEvent() error = %v", err)
	}
	if err := sqliteStore.AppendMetricSnapshot(ctx, storage.MetricSnapshotRecord{
		ID: "m1", AgentID: "a1", InstanceID: "i1", CapturedAt: old,
		Values: map[string]uint64{"v": 1},
	}); err != nil {
		t.Fatalf("AppendMetricSnapshot() error = %v", err)
	}

	server.runTimeseriesRollup(ctx)

	events, _ := sqliteStore.ListAuditEvents(ctx, 0)
	if len(events) != 1 {
		t.Fatalf("expected audit_events row to survive disabled retention, got len=%d", len(events))
	}
	snapshots, _ := sqliteStore.ListMetricSnapshots(ctx)
	if len(snapshots) != 1 {
		t.Fatalf("expected metric_snapshots row to survive disabled retention, got len=%d", len(snapshots))
	}
}
