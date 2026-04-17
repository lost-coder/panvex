package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// -----------------------------------------------------------------------
// P2-LOG-10 / M-R4 / P7-R6: async audit writes + shutdown-flush + alert.
// -----------------------------------------------------------------------

// slowAuditStore wraps a Store and sleeps inside AppendAuditEvent to simulate
// a DB stall. It lets TestAuditWriteIsAsync prove that the HTTP request path
// (Enqueue into the batch buffer) does NOT inherit the stall — enqueue must
// be O(1) and finish in microseconds even when persistence is frozen.
type slowAuditStore struct {
	storage.Store
	stall time.Duration
	calls atomic.Int32
}

func (s *slowAuditStore) AppendAuditEvent(ctx context.Context, event storage.AuditEventRecord) error {
	s.calls.Add(1)
	// Sleep interruptibly so shutdown tests do not deadlock if the caller
	// cancels the context.
	select {
	case <-time.After(s.stall):
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.Store.AppendAuditEvent(ctx, event)
}

// notNullAuditStore wraps a Store and returns a pg NOT NULL violation for
// every AppendAuditEvent. 23502 is classified as "persistent" by
// classifyFlushError, so the batch writer's retry loop short-circuits and
// flushItem logs slog.Error with the critical alert key.
type notNullAuditStore struct {
	storage.Store
}

func (s *notNullAuditStore) AppendAuditEvent(_ context.Context, _ storage.AuditEventRecord) error {
	return &pgconn.PgError{Code: "23502", Message: "null value in column \"actor_id\""}
}

func newAuditRecord(id string) storage.AuditEventRecord {
	return storage.AuditEventRecord{
		ID:        id,
		ActorID:   "user-1",
		Action:    "test.action",
		TargetID:  "target-1",
		CreatedAt: time.Now().UTC(),
	}
}

// TestAuditWriteIsAsync proves the audit-enqueue call site is non-blocking
// even when the underlying store is wedged. The HTTP request path must NEVER
// take longer than the enqueue itself (a mutex + slice append). We pick an
// intentionally lenient budget of 10ms so the assertion is robust on slow
// CI runners; the real observed cost is sub-millisecond.
func TestAuditWriteIsAsync(t *testing.T) {
	base, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })

	slow := &slowAuditStore{Store: base, stall: 100 * time.Millisecond}
	w := newStoreBatchWriter(slow, nil)
	// Do NOT Start(): we only want to prove Enqueue is cheap. Starting would
	// race the test assertion against a background flush that is known to be
	// slow.
	t.Cleanup(func() {
		// Drain the single queued row in a best-effort call so the store's
		// ring buffer does not leak past the test. 200ms is more than
		// enough for one slow insert.
		_ = w.StopWithTimeout(200 * time.Millisecond)
	})

	start := time.Now()
	w.auditEvents.Enqueue(newAuditRecord("evt-async"))
	elapsed := time.Since(start)

	if elapsed > 10*time.Millisecond {
		t.Fatalf("Enqueue took %v, want < 10ms — HTTP path must not block on DB", elapsed)
	}
	// The stall has not been consumed yet — the background loop is not
	// running. This is the point: enqueue returned ~instantly even though
	// the store would have stalled the caller for ~100ms.
}

// TestAuditBufferFlushesOnShutdown enqueues 100 audit rows, calls Stop with a
// generous timeout, and asserts every row landed in the store. This is the
// core graceful-shutdown guarantee required by P2-LOG-10 / M-R4 — a kill
// signal during active audit writes must not lose events.
func TestAuditBufferFlushesOnShutdown(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	w := newStoreBatchWriter(store, nil)
	w.Start()

	const n = 100
	for i := 0; i < n; i++ {
		w.auditEvents.Enqueue(storage.AuditEventRecord{
			ID:        "evt-" + randSuffix(i),
			ActorID:   "user-1",
			Action:    "shutdown.test",
			TargetID:  "target-1",
			CreatedAt: time.Now().UTC(),
		})
	}

	// Stop must drain synchronously; 10s is the production budget.
	if err := w.StopWithTimeout(10 * time.Second); err != nil {
		t.Fatalf("StopWithTimeout: %v", err)
	}

	got, err := store.ListAuditEvents(context.Background(), n+10)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(got) != n {
		t.Fatalf("persisted %d audit events after shutdown, want %d (lost rows!)", len(got), n)
	}
}

// randSuffix returns a short deterministic suffix so audit IDs are unique
// per test iteration without pulling in crypto/rand.
func randSuffix(i int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, 8)
	for j := range b {
		b[j] = hex[(i*7+j*11)&0xf]
	}
	return string(b) + "-" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// TestAuditBufferPersistentErrorAlerts exercises the critical-stream log path.
// When AppendAuditEvent returns a NOT NULL violation (23502 — persistent per
// classifyFlushError), flushItem must emit a slog.Error whose structured
// attributes include alert=audit_persist_failed. Operators use this stable
// key to page, so it must survive schema changes.
func TestAuditBufferPersistentErrorAlerts(t *testing.T) {
	base, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })

	// Install a JSON-encoded slog handler backed by a lockable buffer so we
	// can parse every record the batch writer emits and assert on the
	// structured attrs. SetDefault is process-global; restore the previous
	// logger when the test exits so subsequent tests are not contaminated.
	var logBuf safeBuffer
	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })

	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelError,
	})))

	sink := newRecordingSink()
	w := newTestBatchWriter(&notNullAuditStore{Store: base}, sink)

	w.flushAuditEvents(context.Background(), []storage.AuditEventRecord{
		newAuditRecord("evt-null"),
	})

	// Persistent errors should be counted once and bypass the retry loop.
	if got := sink.flushErrorCount("audit", "persistent"); got != 1 {
		t.Fatalf("persistent flush errors = %d, want 1", got)
	}
	if got := sink.flushErrorCount("audit", "transient"); got != 0 {
		t.Fatalf("transient flush errors = %d, want 0 (23502 is persistent)", got)
	}

	// Scan every JSON log line; at least one must carry
	// alert=audit_persist_failed with level=ERROR.
	found := false
	for _, line := range strings.Split(strings.TrimRight(logBuf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("slog line is not JSON (%q): %v", line, err)
		}
		if rec["level"] != "ERROR" {
			continue
		}
		if rec["alert"] == "audit_persist_failed" && rec["domain"] == "audit" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no slog.Error record with alert=audit_persist_failed; captured logs:\n%s", logBuf.String())
	}
}

// safeBuffer is a bytes.Buffer protected by a mutex so concurrent slog writes
// (the JSON handler serialises records but Write is not atomic at our
// granularity) do not race under -race.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
