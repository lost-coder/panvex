package server

import (
	"context"
	"io"
	"log/slog"
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

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
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

	// P2-DB-03: metric_snapshots.agent_id is now a CASCADE FK to agents(id);
	// seed the referenced agent before inserting snapshot rows.
	if err := sqliteStore.PutAgent(ctx, storage.AgentRecord{
		ID:         "a1",
		NodeName:   "node-rollup",
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
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
	req := httptest.NewRequestWithContext(t.Context(),http.MethodGet, "/metrics", nil)
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

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
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

	// P2-DB-03: metric_snapshots.agent_id is now a CASCADE FK to agents(id).
	if err := sqliteStore.PutAgent(ctx, storage.AgentRecord{
		ID:         "a1",
		NodeName:   "node-rollup-disabled",
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

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

// TestRunTimeseriesRollupPrunesExpiredAgentRevocations (C4): the rollup
// loop must call DeleteExpiredAgentRevocations — the method existed in
// every backend but had no production caller.
func TestRunTimeseriesRollupPrunesExpiredAgentRevocations(t *testing.T) {
	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	ctx := context.Background()

	if err := server.store.PutAgentRevocation(ctx, storage.AgentRevocationRecord{
		AgentID:       "agent-expired",
		RevokedAt:     now.Add(-72 * time.Hour),
		CertExpiresAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("PutAgentRevocation() error = %v", err)
	}

	server.runTimeseriesRollup(ctx)

	remaining, err := server.store.ListAgentRevocations(ctx)
	if err != nil {
		t.Fatalf("ListAgentRevocations() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("revocations after rollup = %d, want 0", len(remaining))
	}
}

// TestRunRetentionPruneWarnsOnceWhenDisabled verifies that a ttlSeconds<=0
// ("keep forever") setting logs a loud WARN the first time runRetentionPrune
// sees it, does NOT error, still no-ops the prune, and does not repeat the
// warning on subsequent ticks while the series stays disabled (avoids log
// spam on every rollup interval).
func TestRunRetentionPruneWarnsOnceWhenDisabled(t *testing.T) {
	now := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	server := testServerWithSQLiteLogger(t, now, logger)
	ctx := context.Background()

	pruneCalls := 0
	pruneFn := func(_ context.Context, _ time.Time) (int64, error) {
		pruneCalls++
		return 0, nil
	}

	// First tick: ttlSeconds<=0 must warn exactly once and must not call
	// pruneFn (the whole point of "disabled" is skip-the-prune).
	server.runRetentionPrune(ctx, "metric_snapshots", now, 0, pruneFn)
	if pruneCalls != 0 {
		t.Fatalf("pruneFn called %d times with ttlSeconds<=0, want 0", pruneCalls)
	}
	out := logBuf.String()
	if !strings.Contains(out, "retention disabled for series") || !strings.Contains(out, "metric_snapshots") {
		t.Fatalf("expected loud disabled-retention warning for metric_snapshots, got log:\n%s", out)
	}
	if got := strings.Count(out, "retention disabled for series"); got != 1 {
		t.Fatalf("expected exactly 1 warning line after first tick, got %d in:\n%s", got, out)
	}

	// Second tick with a negative ttl (still "disabled"): must NOT warn
	// again — this is the log-spam guard.
	server.runRetentionPrune(ctx, "metric_snapshots", now, -1, pruneFn)
	if pruneCalls != 0 {
		t.Fatalf("pruneFn called %d times with ttlSeconds<0, want 0", pruneCalls)
	}
	if got := strings.Count(logBuf.String(), "retention disabled for series"); got != 1 {
		t.Fatalf("expected warning to stay at 1 occurrence across repeated disabled ticks, got %d in:\n%s", got, logBuf.String())
	}
}

// TestRunRetentionPruneNonZeroTTLStillPrunesAndDoesNotWarn is the control
// case: a positive ttlSeconds must still invoke pruneFn and must never log
// the disabled-retention warning.
func TestRunRetentionPruneNonZeroTTLStillPrunesAndDoesNotWarn(t *testing.T) {
	now := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	server := testServerWithSQLiteLogger(t, now, logger)
	ctx := context.Background()

	pruneCalls := 0
	var gotCutoff time.Time
	pruneFn := func(_ context.Context, before time.Time) (int64, error) {
		pruneCalls++
		gotCutoff = before
		return 3, nil
	}

	server.runRetentionPrune(ctx, "metric_snapshots", now, 86400, pruneFn)

	if pruneCalls != 1 {
		t.Fatalf("pruneFn called %d times with ttlSeconds=86400, want 1", pruneCalls)
	}
	wantCutoff := now.Add(-86400 * time.Second)
	if !gotCutoff.Equal(wantCutoff) {
		t.Fatalf("cutoff = %v, want %v", gotCutoff, wantCutoff)
	}
	if strings.Contains(logBuf.String(), "retention disabled for series") {
		t.Fatalf("non-zero TTL must not log the disabled-retention warning, got:\n%s", logBuf.String())
	}
}

// TestRunRetentionPruneWarnsAgainAfterReenable verifies that once a series
// transitions disabled -> enabled -> disabled again, the warning fires again
// on the second disable instead of staying silent forever (an operator who
// re-disables retention after a period of pruning should still be warned).
func TestRunRetentionPruneWarnsAgainAfterReenable(t *testing.T) {
	now := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	server := testServerWithSQLiteLogger(t, now, logger)
	ctx := context.Background()
	pruneFn := func(_ context.Context, _ time.Time) (int64, error) { return 0, nil }

	server.runRetentionPrune(ctx, "jobs", now, 0, pruneFn)    // disable #1: warns
	server.runRetentionPrune(ctx, "jobs", now, 3600, pruneFn) // re-enable: prunes
	server.runRetentionPrune(ctx, "jobs", now, 0, pruneFn)    // disable #2: should warn again

	if got := strings.Count(logBuf.String(), "retention disabled for series"); got != 2 {
		t.Fatalf("expected 2 warnings across disable/enable/disable cycle, got %d in:\n%s", got, logBuf.String())
	}
}
