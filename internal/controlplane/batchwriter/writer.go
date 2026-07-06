package batchwriter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const (
	batchFlushInterval = 500 * time.Millisecond
	// defaultBatchMaxSize is the fallback per-stream buffer size used when
	// streamBatchSizes does not list the stream explicitly. SQLite and
	// PostgreSQL both prefer 200–1000 rows per multi-row INSERT for the
	// hot streams; 200 keeps the bound memory footprint reasonable for
	// streams whose volume profile we have not measured.
	defaultBatchMaxSize = 200

	// defaultAuditDeadLetterDir is the on-disk directory where audit batches
	// that exhaust their in-memory flush retries are persisted as JSONL so a
	// permanent store failure cannot SILENTLY lose audit events (A4). It is a
	// relative path resolved against the process working directory — the same
	// convention as the workspace `data/` runtime dir — because the
	// control-plane server has no first-class data-dir abstraction to thread
	// through. Override via the Writer.deadLetterDir field.
	defaultAuditDeadLetterDir = "data/audit-deadletter"

	// logBatchFlush is the debug log message emitted once per stream flush.
	logBatchFlush = "batch flush"
)

// streamBatchSizes is the per-stream flush threshold (in items) used by
// newBatchBuffer to size each typed batchBuffer. The previous global
// batchMaxSize=50 forced every stream to flush 50 rows at a time, which on
// PostgreSQL meant ~200 round-trips/sec under fleet telemetry load. Bulk
// INSERT efficiency tops out around 500–1000 rows on both drivers, while
// audit and fallback_state remain low-volume / latency-sensitive — keep
// those at 50 so audit log lines surface within one flush interval.
//
// Streams not listed here use defaultBatchMaxSize. Time-based flushing is
// unchanged: any stream still flushes every batchFlushInterval regardless
// of size.
var streamBatchSizes = map[string]int{
	"telemetry":      500, // fleet runtime/diag/security parts, dominant traffic
	"client_ips":     500, // per-client IP history rows, very chatty
	"dc_health":      500, // per-DC health point timeseries, high-rate
	"audit":          50,  // low-frequency, small batch keeps latency low
	"fallback_state": 50,  // critical, must persist promptly
}

// batchSizeFor returns the configured per-stream flush threshold, falling
// back to defaultBatchMaxSize when the stream is not in streamBatchSizes.
func batchSizeFor(stream string) int {
	if n, ok := streamBatchSizes[stream]; ok {
		return n
	}
	return defaultBatchMaxSize
}

// retryBackoffs defines the exponential backoff schedule used when retrying
// transient flush errors (P2-REL-06 / remediation finding H14). Each entry is
// the delay BEFORE the next attempt, so the full schedule is:
//
//	attempt 1 -> immediate
//	attempt 2 -> after 100ms
//	attempt 3 -> after 500ms
//
// Max 3 attempts total. After the final attempt the retry sequence is
// reported as "exhausted" and the item is dropped — the batch loop continues
// so a single poisoned record cannot stall the rest of the batch.
var retryBackoffs = []time.Duration{
	100 * time.Millisecond,
	500 * time.Millisecond,
}

// MetricsSink is the narrow interface Writer needs from the
// Prometheus collector bundle. Kept small so tests can inject a fake without
// having to construct a full *metricsCollectors.
//
// ObserveFlushError is incremented once per failed flush attempt, with type
// == "transient" or "persistent".
//
// ObservePersistRetry is incremented once per item whose retry sequence
// resolved, with outcome == "success" (retry eventually succeeded) or
// "exhausted" (all retries failed, item dropped).
//
// ObserveFlushDuration is called once per item after its retry sequence
// resolves, regardless of outcome (success, exhausted, or persistent drop),
// with the total wall-clock seconds spent across all attempts.
type MetricsSink interface {
	ObserveFlushError(buffer, errorType string)
	SetQueueDepth(buffer string, depth float64)
	ObservePersistRetry(stream, outcome string)
	ObserveFlushDuration(stream string, seconds float64)
	// ObserveBufferDrop is incremented by n every time the bounded buffer
	// evicts items under the drop-oldest overflow policy (P6-6.1c, #11).
	ObserveBufferDrop(buffer string, n int)
}

// batchBuffer accumulates items of type T and flushes them either on a timer
// or when the buffer reaches a size threshold. The flush function runs outside
// the buffer's own lock, so long-running DB writes do not block enqueue callers.
//
// P6-6.1c (finding #11): the buffer is bounded by capLimit with a
// drop-oldest policy. The live window is items[start:]; on overflow the
// head index advances (O(1)) and the evicted element is reported via
// onDropped/observeDrop. When the dead prefix grows past capLimit the
// backing slice is compacted in one copy — amortised O(1) per enqueue,
// bounded memory of ~2×capLimit elements. capLimit <= 0 disables the bound
// (unbounded, prior behaviour).
type batchBuffer[T any] struct {
	mu       sync.Mutex
	items    []T
	start    int // logical head: items[start:] are live
	maxSize  int // flush threshold (per-stream, streamBatchSizes)
	capLimit int // hard bound on live items; <=0 = unbounded
	flushFn  func(ctx context.Context, items []T)
	signal   chan struct{}
	// onDropped, when non-nil, receives every item evicted by the
	// drop-oldest policy. Invoked OUTSIDE the buffer lock. Used by the
	// audit stream to spool evicted events to the dead-letter file.
	onDropped func(item T)
	// observeDrop, when non-nil, reports the number of evicted items to
	// the metrics sink (panvex_batch_dropped_total). Outside the lock.
	observeDrop func(n int)
}

func newBatchBuffer[T any](maxSize int, flush func(ctx context.Context, items []T)) *batchBuffer[T] {
	return &batchBuffer[T]{
		items:   make([]T, 0, maxSize),
		maxSize: maxSize,
		flushFn: flush,
		signal:  make(chan struct{}, 1),
	}
}

// Enqueue appends an item to the buffer. If the buffer is at capLimit the
// OLDEST item is evicted (drop-oldest keeps the newest data — for
// telemetry-style streams the most recent snapshot is the valuable one).
// If the live window reaches maxSize a non-blocking flush signal fires.
func (b *batchBuffer[T]) Enqueue(item T) {
	var evicted T
	overflow := false

	b.mu.Lock()
	b.items = append(b.items, item)
	if b.capLimit > 0 && len(b.items)-b.start > b.capLimit {
		evicted = b.items[b.start]
		var zero T
		b.items[b.start] = zero // release the reference for GC
		b.start++
		overflow = true
		if b.start > b.capLimit {
			// Compact: slide the live window to the front so the backing
			// array never exceeds ~2×capLimit elements.
			n := copy(b.items, b.items[b.start:])
			tail := b.items[n:]
			for i := range tail {
				var zero T
				tail[i] = zero
			}
			b.items = b.items[:n]
			b.start = 0
		}
	}
	full := len(b.items)-b.start >= b.maxSize
	b.mu.Unlock()

	if overflow {
		if b.observeDrop != nil {
			b.observeDrop(1)
		}
		if b.onDropped != nil {
			b.onDropped(evicted)
		}
	}
	if full {
		select {
		case b.signal <- struct{}{}:
		default:
		}
	}
}

// Len returns the current number of queued (live) items. Used by the flush
// loop to sample the queue-depth gauge before draining.
func (b *batchBuffer[T]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items) - b.start
}

// Drain swaps out the current live window and flushes accumulated items. Safe
// to call concurrently — only one drain runs at a time because items are
// swapped under the lock before the flush function is invoked.
func (b *batchBuffer[T]) Drain(ctx context.Context) {
	b.mu.Lock()
	if len(b.items)-b.start == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.items[b.start:]
	b.items = make([]T, 0, b.maxSize)
	b.start = 0
	b.mu.Unlock()

	b.flushFn(ctx, batch)
}

// Writer decouples storage I/O from the server's in-memory state
// mutex by accumulating writes in typed buffers and flushing them to the store
// on a background timer. This eliminates DB latency from the critical path.
type Writer struct {
	store   storage.Store
	metrics MetricsSink
	// done is closed by StopWithTimeout to signal the background flush
	// loop and any in-flight retry sleeps that the writer is shutting
	// down. Closing-only semantics replace what used to be an embedded
	// context.Context field (S8242).
	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	// flushInterval is the ticker cadence for the background flush loop.
	// Defaults to batchFlushInterval (500ms). May be overridden before
	// Start() is called so tests and the operational-settings store can
	// tune the interval without recompiling.
	flushInterval time.Duration

	// sleep is the backoff sleeper used by retryWithBackoff. Exported through
	// a field so tests can override it to zero-duration sleeps without
	// blocking the test for seconds.
	sleep func(d time.Duration)

	// now is the injectable clock used by flushItem to measure flush latency.
	// Stays a field (not a package-level var) so tests can supply a mock clock
	// to assert deterministic ObserveFlushDuration values.
	now func() time.Time

	// deadLetterDir is the directory where audit batches that exhaust their
	// in-memory flush retries are spooled as JSONL (A4). Telemetry/timeseries
	// streams are NOT spooled — for them a drop-with-metric is acceptable, but
	// audit is a CRITICAL stream and must never be lost silently. Empty falls
	// back to defaultAuditDeadLetterDir.
	deadLetterDir string

	// writeDeadLetter is the injection seam used by flushAuditEvents to spool a
	// failed audit record. Defaults to writeAuditDeadLetter; tests override it
	// to capture spooled records (or to force a dead-letter write failure).
	writeDeadLetter func(item storage.AuditEventRecord) error

	agents     *batchBuffer[storage.AgentRecord]
	instances  *batchBuffer[storage.InstanceRecord]
	metricsBuf *batchBuffer[storage.MetricSnapshotRecord]
	serverLoad *batchBuffer[storage.ServerLoadPointRecord]
	dcHealth   *batchBuffer[storage.DCHealthPointRecord]
	clientIPs  *batchBuffer[storage.ClientIPHistoryRecord]
	telemetry  *batchBuffer[TelemetryWriteUnit]
	// auditEvents is the 8th stream (P2-LOG-10 / M-R4 / P7-R6). Audit writes
	// used to run synchronously on the HTTP request path — a stalled DB froze
	// login and other handlers. Appending into this buffer is O(1) and the
	// background flush loop persists rows asynchronously with the shared
	// retry/classify logic. Critical-stream alerts are emitted via
	// StreamAlerts[audit] so operators can page on persistent audit failures.
	auditEvents *batchBuffer[storage.AuditEventRecord]
	// fallbackState carries put/delete ops for agent_fallback_state. The
	// authoritative in-memory copy lives in s.fallback (agents.FallbackTracker);
	// this buffer only persists transitions for restart durability.
	fallbackState *batchBuffer[fallbackStateOp]
}

// fallbackStateOp is one queued mutation against agent_fallback_state. Either
// a put (record entered_at when an agent transitions into ME→Direct fallback)
// or a delete (clear when fallback ends). Persisted asynchronously by the
// batch writer; the in-memory map on Server is the authoritative read source
// during the flush window.
type fallbackStateOp struct {
	agentID   string
	enteredAt time.Time
	op        string // "put" or "delete"
}

// TelemetryWriteUnit groups all per-agent telemetry writes for a single
// snapshot so they can be flushed together.
type TelemetryWriteUnit struct {
	AgentID     string
	Runtime     *storage.TelemetryRuntimeCurrentRecord
	DCs         []storage.TelemetryRuntimeDCRecord
	Upstreams   []storage.TelemetryRuntimeUpstreamRecord
	Events      []storage.TelemetryRuntimeEventRecord
	Diagnostics *storage.TelemetryDiagnosticsCurrentRecord
	Security    *storage.TelemetrySecurityInventoryCurrentRecord
}

// noopMetricsSink is the default sink used when a caller does not supply one
// (e.g. legacy tests constructing the writer with nil metrics). It keeps the
// writer usable without the metrics subsystem wired in.
type noopMetricsSink struct{}

// noopMetricsSink implements MetricsSink as a null-object so the
// writer can run without a metrics subsystem wired in. Each method is
// intentionally a no-op — see the type comment above for context.
func (noopMetricsSink) ObserveFlushError(string, string)     { /* null-object */ }
func (noopMetricsSink) SetQueueDepth(string, float64)        { /* null-object */ }
func (noopMetricsSink) ObservePersistRetry(string, string)   { /* null-object */ }
func (noopMetricsSink) ObserveFlushDuration(string, float64) { /* null-object */ }
func (noopMetricsSink) ObserveBufferDrop(string, int)        { /* null-object */ }

func New(store storage.Store, metrics MetricsSink, now func() time.Time) *Writer {
	if metrics == nil {
		metrics = noopMetricsSink{}
	}
	if now == nil {
		now = time.Now
	}
	w := &Writer{
		store:         store,
		metrics:       metrics,
		done:          make(chan struct{}),
		flushInterval: batchFlushInterval,
		sleep:         time.Sleep,
		now:           now,
		deadLetterDir: defaultAuditDeadLetterDir,
	}
	// Default the dead-letter spool to the on-disk JSONL writer. Tests override
	// w.writeDeadLetter to capture records without touching the filesystem.
	w.writeDeadLetter = w.writeAuditDeadLetter

	w.agents = newBatchBuffer(batchSizeFor("agents"), w.flushAgents)
	w.instances = newBatchBuffer(batchSizeFor("instances"), w.flushInstances)
	w.metricsBuf = newBatchBuffer(batchSizeFor("metrics"), w.flushMetrics)
	w.serverLoad = newBatchBuffer(batchSizeFor("server_load"), w.flushServerLoad)
	w.dcHealth = newBatchBuffer(batchSizeFor("dc_health"), w.flushDCHealth)
	w.clientIPs = newBatchBuffer(batchSizeFor("client_ips"), w.flushClientIPs)
	w.telemetry = newBatchBuffer(batchSizeFor("telemetry"), w.flushTelemetry)
	w.auditEvents = newBatchBuffer(batchSizeFor("audit"), w.flushAuditEvents)
	w.fallbackState = newBatchBuffer(batchSizeFor("fallback_state"), w.flushFallbackState)

	w.SetBufferCap(defaultBatchBufferCap)
	w.wireDropObservers()

	return w
}

// defaultBatchBufferCap bounds every stream buffer (P6-6.1c, finding #11).
// Overridable via the storage.batch_buffer_cap operational setting.
const defaultBatchBufferCap = 10_000

// SetBufferCap applies one hard bound to every stream buffer. Must be
// called before Start(); the operational-settings store supplies the
// value in lifecycle.go next to flushInterval.
func (w *Writer) SetBufferCap(capLimit int) {
	w.agents.capLimit = capLimit
	w.instances.capLimit = capLimit
	w.metricsBuf.capLimit = capLimit
	w.serverLoad.capLimit = capLimit
	w.dcHealth.capLimit = capLimit
	w.clientIPs.capLimit = capLimit
	w.telemetry.capLimit = capLimit
	w.auditEvents.capLimit = capLimit
	w.fallbackState.capLimit = capLimit
}

// SetFlushInterval overrides the background flush cadence. Must be called
// before Start; lifecycle wiring reads it from operational settings.
func (w *Writer) SetFlushInterval(d time.Duration) { w.flushInterval = d }

// Enqueue* are the typed entry points other packages use to append records to
// each stream buffer. They replace direct access to the (now unexported)
// buffer fields, keeping the buffer wiring internal to this package.
func (w *Writer) EnqueueAgent(rec storage.AgentRecord)                { w.agents.Enqueue(rec) }
func (w *Writer) EnqueueInstance(rec storage.InstanceRecord)          { w.instances.Enqueue(rec) }
func (w *Writer) EnqueueMetric(rec storage.MetricSnapshotRecord)      { w.metricsBuf.Enqueue(rec) }
func (w *Writer) EnqueueServerLoad(rec storage.ServerLoadPointRecord) { w.serverLoad.Enqueue(rec) }
func (w *Writer) EnqueueDCHealth(rec storage.DCHealthPointRecord)     { w.dcHealth.Enqueue(rec) }
func (w *Writer) EnqueueClientIP(rec storage.ClientIPHistoryRecord)   { w.clientIPs.Enqueue(rec) }
func (w *Writer) EnqueueAudit(rec storage.AuditEventRecord)           { w.auditEvents.Enqueue(rec) }
func (w *Writer) EnqueueTelemetry(u TelemetryWriteUnit)               { w.telemetry.Enqueue(u) }

// Flush synchronously drains every pending buffer to the store. Safe to call
// while the background loops run — each buffer drain is mutex-guarded — and
// used by tests that need queued writes visible in the store immediately.
func (w *Writer) Flush(ctx context.Context) { w.drainAll(ctx) }

// wireDropObservers connects every buffer's overflow path to the metrics
// sink, and routes evicted AUDIT events into the dead-letter spool so the
// critical stream never drops silently (A4). fallback_state eviction is
// safe by construction: put/delete ops are full per-agent overwrites, so
// the newest op per agent supersedes any evicted older one.
func (w *Writer) wireDropObservers() {
	w.agents.observeDrop = func(n int) { w.metrics.ObserveBufferDrop("agents", n) }
	w.instances.observeDrop = func(n int) { w.metrics.ObserveBufferDrop("instances", n) }
	w.metricsBuf.observeDrop = func(n int) { w.metrics.ObserveBufferDrop("metrics", n) }
	w.serverLoad.observeDrop = func(n int) { w.metrics.ObserveBufferDrop("server_load", n) }
	w.dcHealth.observeDrop = func(n int) { w.metrics.ObserveBufferDrop("dc_health", n) }
	w.clientIPs.observeDrop = func(n int) { w.metrics.ObserveBufferDrop("client_ips", n) }
	w.telemetry.observeDrop = func(n int) { w.metrics.ObserveBufferDrop("telemetry", n) }
	w.auditEvents.observeDrop = func(n int) { w.metrics.ObserveBufferDrop("audit", n) }
	w.fallbackState.observeDrop = func(n int) { w.metrics.ObserveBufferDrop("fallback_state", n) }
	w.auditEvents.onDropped = func(item storage.AuditEventRecord) {
		if err := w.writeDeadLetter(item); err != nil {
			slog.Error("audit overflow dead-letter write failed",
				"domain", "audit",
				"audit_id", item.ID,
				"error", err,
				"alert", "audit_deadletter_write_failed",
			)
		}
	}
}

// Start launches the background flush goroutine.
//
// parentCtx is the writer's lifecycle context — typically Server.serverCtx so
// in-flight drains inherit cancellation when the server shuts down. The flush
// loop derives a local cancellable ctx from it that is also tripped when
// w.done is closed (StopWithTimeout). The shutdown final-drain runs on its
// own caller-supplied parentCtx via StopWithTimeout and is unaffected by this
// cancellation. parentCtx may be nil/Background only in tests; production
// callers must pass a real lifecycle ctx (Plan 3 tail / S25 T2).
//
//nolint:contextcheck // test-only nil parentCtx triggers a Background fallback; production callers always supply serverCtx.
func (w *Writer) Start(parentCtx context.Context) {
	if parentCtx == nil {
		parentCtx = context.WithoutCancel(context.Background()) //nolint:noctx,contextcheck // reason: test-only fallback when Start is called without a lifecycle ctx; production wiring (lifecycle.go) always supplies serverCtx.
	}
	w.wg.Add(2)
	go w.flushLoop(parentCtx)
	go w.criticalFlushLoop(parentCtx)
}

// Stop cancels the background goroutine, waits for it to exit, then performs
// a final drain of all buffers so pending writes are persisted before the
// store is closed. Q4.U-Q-17 removed the deprecated Stop() in favour of
// StopWithTimeout — every shutdown path now states its budget explicitly.

// StopWithTimeout performs a graceful shutdown of the batch writer. It
// cancels the background flush loop, waits for it to exit, then performs a
// final synchronous drain of every buffer so queued rows are persisted
// before the caller proceeds to close the underlying store. The drain is
// bounded by `timeout` — if it does not complete in time, StopWithTimeout
// returns a context.DeadlineExceeded error so the caller can record the
// partial-flush condition (we do not panic or block indefinitely).
//
// parentCtx is the shutdown-scope ctx the timeout is layered on top of. It
// MUST NOT be a ctx that the caller has already cancelled (e.g. a server
// lifecycle ctx that Close cancelled prior to this call) — the drain would
// abort immediately and queued audit rows would be lost. Callers that need
// to detach from a cancelled lifecycle ctx should pass
// context.WithoutCancel(serverCtx) so values propagate but cancellation
// does not. Plan 3 / BP-01 routes the literal Background away from this
// path; tests and call sites that have no lifecycle ctx still pass
// context.Background() at the call edge.
//
// Events enqueued AFTER StopWithTimeout begins are still accepted by the
// lock-free Enqueue path, but they may be dropped if the drain goroutine is
// already past the per-buffer Drain call — callers that must not lose events
// should stop upstream producers (HTTP handlers, gRPC streams) before
// invoking StopWithTimeout.
func (w *Writer) StopWithTimeout(parentCtx context.Context, timeout time.Duration) error {
	w.stopOnce.Do(func() { close(w.done) })
	w.wg.Wait()

	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.drainAll(ctx)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		// Wait for the drain goroutine to finish its current flushItem call
		// so it does not race with store.Close(). flushItem itself cannot
		// block forever because ClassifyFlushError + retry will bail on
		// context.DeadlineExceeded.
		<-done
		return ctx.Err()
	}
}

func (w *Writer) flushLoop(parentCtx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	// flushCtx inherits VALUES from parentCtx (Server.serverCtx in
	// production — tracing spans, request IDs) but NOT cancellation:
	// the writer's own `w.done` channel is the shutdown signal, not
	// parentCtx cancellation.
	//
	// Why detach cancellation: Server.Close() cancels serverCtx first so
	// other workers observe shutdown promptly, then calls StopWithTimeout
	// to drain. If flushCtx tracked serverCtx cancellation, an in-flight
	// drainAll at the moment Close() fires (e.g. an audit batch that was
	// signalled microseconds before) would see ctx.Done() inside the
	// retry loop, get marked retry_exhausted, and the rows would be lost.
	// Detaching means any drain already in progress completes against
	// the original DB connection regardless of serverCtx cancel.
	//
	// flushCtx is NOT cancelled when w.done closes either: graceful
	// StopWithTimeout (close(done) → wg.Wait()) lets any in-flight drain
	// complete; the loop checks `case <-w.done` only between iterations.
	// Cancelling on done would abort the final inline drain mid-flush
	// and drop rows (regression caught by TestAuditBufferFlushesOnShutdown).
	// The shutdown final-drain runs separately in StopWithTimeout on its
	// own caller-supplied ctx after this loop returns.
	flushCtx := context.WithoutCancel(parentCtx)

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
		case <-w.agents.signal:
		case <-w.instances.signal:
		case <-w.metricsBuf.signal:
		case <-w.serverLoad.signal:
		case <-w.dcHealth.signal:
		case <-w.clientIPs.signal:
		case <-w.telemetry.signal:
		case <-w.auditEvents.signal:
		}
		// Any signal (including ticker) triggers a drain of all regular
		// buffers. This prevents signal coalescing from letting individual
		// buffers grow beyond its per-stream batch size for more than one
		// flush cycle. fallback_state is drained by criticalFlushLoop.
		w.drainRegular(flushCtx)
	}
}

// criticalFlushLoop is the dedicated drain goroutine for the CRITICAL
// fallback_state stream (P6-6.1d, finding #11). Telemetry/audit bulk
// flushes with their retry backoffs no longer sit between an agent's
// ME→Direct transition and its persistence: fallback_state gets its own
// ticker + signal wake-up. Same flushCtx detachment rationale as flushLoop.
func (w *Writer) criticalFlushLoop(parentCtx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	flushCtx := context.WithoutCancel(parentCtx)
	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
		case <-w.fallbackState.signal:
		}
		w.metrics.SetQueueDepth("fallback_state", float64(w.fallbackState.Len()))
		w.fallbackState.Drain(flushCtx)
	}
}

// observeQueueDepths samples every buffer's pending count and publishes it to
// the Prometheus gauge. Called BEFORE drain so the gauge reflects the peak
// queue depth observed by Prometheus (post-drain the value would always be
// zero and useless for alerting).
func (w *Writer) observeQueueDepths() {
	w.metrics.SetQueueDepth("agents", float64(w.agents.Len()))
	w.metrics.SetQueueDepth("instances", float64(w.instances.Len()))
	w.metrics.SetQueueDepth("metrics", float64(w.metricsBuf.Len()))
	w.metrics.SetQueueDepth("server_load", float64(w.serverLoad.Len()))
	w.metrics.SetQueueDepth("dc_health", float64(w.dcHealth.Len()))
	w.metrics.SetQueueDepth("client_ips", float64(w.clientIPs.Len()))
	w.metrics.SetQueueDepth("telemetry", float64(w.telemetry.Len()))
	w.metrics.SetQueueDepth("audit", float64(w.auditEvents.Len()))
	w.metrics.SetQueueDepth("fallback_state", float64(w.fallbackState.Len()))
}

// drainRegular drains every stream EXCEPT fallback_state, which is owned
// by criticalFlushLoop. Ordering rationale unchanged: audit last so rows
// it references land first.
func (w *Writer) drainRegular(ctx context.Context) {
	w.observeQueueDepths()
	w.agents.Drain(ctx)
	w.instances.Drain(ctx)
	w.metricsBuf.Drain(ctx)
	w.serverLoad.Drain(ctx)
	w.dcHealth.Drain(ctx)
	w.clientIPs.Drain(ctx)
	w.telemetry.Drain(ctx)
	// Drain audit last so earlier streams (which may describe the same
	// state transitions being audited) land in the store before the audit
	// rows that reference them. Ordering is best-effort — the store does
	// not enforce cross-table FKs for audit — but it keeps logs readable
	// when operators correlate rows by timestamp.
	w.auditEvents.Drain(ctx)
}

// drainAll is the shutdown-path drain (StopWithTimeout): regular streams
// plus fallback_state. Both loops have already exited (wg.Wait), so there
// is no concurrent Drain.
func (w *Writer) drainAll(ctx context.Context) {
	w.drainRegular(ctx)
	w.fallbackState.Drain(ctx)
}

// ClassifyFlushError returns "transient" or "persistent" for an error produced
// by a Store write.
//
// Strategy:
//  1. Typed checks first — errors.Is against the stable sentinels we know
//     (context.DeadlineExceeded, context.Canceled, sql.ErrConnDone, driver
//     bad-connection aliases).
//  2. String-match fallback — pgx and modernc.org/sqlite surface transport
//     failures without stable sentinel errors. We match case-insensitively on
//     well-known substrings ("connection refused", "timeout", "deadline
//     exceeded", "driver: bad connection"). This is intentionally lenient
//     because the cost of a false-positive (one extra retry) is much lower
//     than the cost of a false-negative (data dropped on what was really a
//     transient blip).
//
// Anything else — constraint violations, missing tables, schema mismatches —
// is classified as "persistent" because retrying cannot help.
func ClassifyFlushError(err error) string {
	if err == nil {
		// callers must not pass err=nil, this fallback is defensive only
		return "persistent"
	}

	// Typed checks first — these are the stable sentinels.
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, sql.ErrConnDone) ||
		errors.Is(err, sql.ErrTxDone) {
		return "transient"
	}

	// net.OpError wraps the underlying network op. We retry any OpError that
	// is tagged as a temporary/timeout condition OR that unwraps to a
	// connection-refused / reset. The stdlib's net package no longer marks
	// many errors as Temporary (see Go issue 29746), so we also inspect the
	// wrapped message.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return "transient"
		}
		// Any OpError short of a permanent DNS failure is worth retrying —
		// TCP resets, refused connects, broken pipes. These all fall out of
		// the string-match branch below.
	}

	// PostgreSQL SQLSTATE codes surfaced by pgx/pgconn. Only the connection-
	// class codes (08xxx) and serialization_failure (40001) are transient.
	// Everything else (constraint violation, undefined table, etc.) is a
	// persistent application error.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "08000", // connection_exception
			"08003", // connection_does_not_exist
			"08006", // connection_failure
			"08001", // sqlclient_unable_to_establish_sqlconnection
			"08004", // sqlserver_rejected_establishment_of_sqlconnection
			"08007", // transaction_resolution_unknown
			"40001", // serialization_failure
			"40P01": // deadlock_detected
			return "transient"
		}
		return "persistent"
	}

	// Fallback string match. Lowercased so driver-specific capitalisation
	// ("EOF", "Connection refused", ...) does not slip through. modernc.org
	// /sqlite returns SQLITE_BUSY / SQLITE_LOCKED as strings with those
	// exact tokens, so we match both upper and lower forms.
	msg := strings.ToLower(err.Error())
	transientMarkers := []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"no route to host",
		"network is unreachable",
		"deadline exceeded",
		"timeout",
		"driver: bad connection",
		"i/o timeout",
		"database is locked",
		"database table is locked",
		"sqlite_busy",
		"sqlite_locked",
		"server closed",
		"eof",
	}
	for _, m := range transientMarkers {
		if strings.Contains(msg, m) {
			return "transient"
		}
	}
	return "persistent"
}

// retryOutcome summarises how a retry sequence ended. It is consumed both by
// the metrics sink (ObservePersistRetry) and by the logging branch in
// flushItem, so the two stay in sync.
type retryOutcome int

const (
	retrySucceededFirstTry retryOutcome = iota // op returned nil on attempt 1 — no retry recorded
	retrySucceededOnRetry                      // op failed then succeeded — outcome=success
	retryExhausted                             // all attempts failed with transient errors
	retryPersistent                            // non-retriable error seen, retries skipped
)

// retryWithBackoff invokes op up to len(retryBackoffs)+1 times (max 3
// attempts per spec), sleeping the scheduled backoff between attempts when
// op returns a transient error. It returns a retryOutcome describing how the
// sequence terminated along with the final error (nil on success).
//
// Each failed attempt increments the per-stream transient counter via
// observeTransient. The final outcome is reported separately by the caller
// using ObservePersistRetry so operators can distinguish "eventually
// succeeded" from "gave up and dropped".
func (w *Writer) retryWithBackoff(op func() error, observeTransient func()) (retryOutcome, error) {
	err := op()
	if err == nil {
		return retrySucceededFirstTry, nil
	}
	if ClassifyFlushError(err) == "persistent" {
		return retryPersistent, err
	}

	for _, delay := range retryBackoffs {
		// Record the failed-and-retrying attempt as a transient observation.
		if observeTransient != nil {
			observeTransient()
		}
		// Abort retry loop if the writer is shutting down — no point sleeping
		// for hundreds of ms when the process is exiting.
		select {
		case <-w.done:
			return retryExhausted, err
		default:
		}
		w.sleep(delay)
		err = op()
		if err == nil {
			return retrySucceededOnRetry, nil
		}
		if ClassifyFlushError(err) == "persistent" {
			return retryPersistent, err
		}
	}
	// All attempts burned and the last error was still transient.
	if observeTransient != nil {
		observeTransient()
	}
	return retryExhausted, err
}

// flushItem is the shared helper every per-buffer flush function uses. It
// retries transient errors, logs + counts persistent ones, and never aborts
// the outer loop so a single poisoned item cannot block the rest of the
// batch. A retry that eventually succeeds is recorded as
// ObservePersistRetry(stream, "success"); a retry that exhausts the schedule
// is recorded as "exhausted" and the item is dropped.
//
// StreamAlerts is the set of streams that warrant a "critical alert" marker
// on the error log line — currently reserved for the audit stream once it
// moves into the batch writer (see P2-LOG-10). For now no buffer is in this
// set, but the hook is here so the plumbing exists and only the list needs
// updating when audit async writes land.
var StreamAlerts = map[string]string{
	// P2-LOG-10 / M-R4 / P7-R6: audit is the CRITICAL stream. Persistent
	// failures (NOT NULL violation, schema mismatch, retry-exhausted
	// transient) emit slog.Error with alert=audit_persist_failed so
	// operator paging rules can match a single stable key.
	"audit": "audit_persist_failed",
	// fallback_state is a CRITICAL stream too: a missed put/delete here
	// silently drifts the in-memory s.fallback (agents.FallbackTracker) from the
	// persisted row, which in turn drifts the 30-min severity boundary
	// after a control-plane restart. Surface flush failures via the same
	// alert-key channel so operators can page on a single stable key
	// (cold-start hydrate failures in restoreFallbackState use the same
	// key — see state_restore.go).
	"fallback_state": "fallback_state_persist_failed",
	// IN-L2: time-series streams. A persistent flush failure drops the batch
	// (rows are not re-buffered), leaving silent gaps in the trend graphs and
	// the hourly rollups derived from them. Surface them on stable alert keys
	// so operators can page on sustained telemetry-persistence loss instead of
	// only seeing a lone Error log line.
	"server_load": "server_load_persist_failed",
	"telemetry":   "telemetry_persist_failed",
	"metrics":     "metrics_persist_failed",
}

func (w *Writer) flushItem(buffer string, logAttrs []any, op func() error) {
	w.flushItemTracked(buffer, logAttrs, op)
}

// flushItemTracked is flushItem with a return value: it reports whether the
// item was ultimately persisted (true) or dropped after the retry sequence
// resolved unsuccessfully (false). Critical streams (audit) use the bool to
// decide whether to spool the record to the dead-letter file instead of
// dropping it silently; non-critical streams call flushItem and ignore it.
func (w *Writer) flushItemTracked(buffer string, logAttrs []any, op func() error) bool {
	// P2-OBS-03: measure end-to-end flush latency including retries and record
	// the observation regardless of how the retry sequence resolved. Done via
	// a deferred closure so early returns (first-try success, retry success)
	// still emit a sample.
	start := w.now()
	defer func() {
		w.metrics.ObserveFlushDuration(buffer, w.now().Sub(start).Seconds())
	}()

	outcome, err := w.retryWithBackoff(op, func() {
		w.metrics.ObserveFlushError(buffer, "transient")
	})
	switch outcome {
	case retrySucceededFirstTry:
		return true
	case retrySucceededOnRetry:
		// Retry saved us — record the positive outcome but do not log.
		w.metrics.ObservePersistRetry(buffer, "success")
		return true
	case retryExhausted:
		w.metrics.ObservePersistRetry(buffer, "exhausted")
		w.metrics.ObserveFlushError(buffer, "persistent")
	case retryPersistent:
		w.metrics.ObserveFlushError(buffer, "persistent")
	}

	attrs := append([]any{
		"domain", buffer,
		"outcome", outcomeLabel(outcome),
		"error", err,
		"error_chain", errorChain(err),
	}, logAttrs...)
	if alert, ok := StreamAlerts[buffer]; ok {
		attrs = append(attrs, "alert", alert)
	}
	slog.Error("batch persist failed", attrs...)
	return false
}

func outcomeLabel(o retryOutcome) string {
	switch o {
	case retryExhausted:
		return "retry_exhausted"
	case retryPersistent:
		return "persistent"
	default:
		return "unknown"
	}
}

// errorChain walks the wrapped-error chain and returns a flat slice of
// messages from outermost to innermost. Useful for audit logs where the
// original cause is often hidden under two or three wrapping layers.
func errorChain(err error) []string {
	var out []string
	for err != nil {
		out = append(out, err.Error())
		err = errors.Unwrap(err)
	}
	return out
}

// Flush functions — each persists all accumulated items through the Store's
// bulk insert API in a single transaction (P3-PERF-01a / H13). The retry +
// classification + metrics machinery from flushItem wraps the bulk call so a
// transient error retries the whole batch, while a persistent error drops it
// and records `ObservePersistRetry(stream, "exhausted"|"persistent")` exactly
// once (not once per row) — that matches operator expectations: one batch,
// one outcome.
//
// Previously these flushers looped over each item and called the single-row
// Store method, which on a 50-row batch produced 50 network round-trips and
// dominated the PERF-05 baseline (~5.5ms/flush). Bulk INSERT collapses the
// round-trips to O(chunks) where chunks = ceil(items / 500).

func (w *Writer) flushAgents(ctx context.Context, items []storage.AgentRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "agents", "count", len(items))
	w.flushItem("agents", []any{"count", len(items)}, func() error {
		return w.store.PutAgentsBulk(ctx, items)
	})
}

func (w *Writer) flushInstances(ctx context.Context, items []storage.InstanceRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "instances", "count", len(items))
	w.flushItem("instances", []any{"count", len(items)}, func() error {
		return w.store.PutInstancesBulk(ctx, items)
	})
}

func (w *Writer) flushMetrics(ctx context.Context, items []storage.MetricSnapshotRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "metrics", "count", len(items))
	w.flushItem("metrics", []any{"count", len(items)}, func() error {
		return w.store.AppendMetricSnapshotsBulk(ctx, items)
	})
}

func (w *Writer) flushServerLoad(ctx context.Context, items []storage.ServerLoadPointRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "server_load", "count", len(items))
	w.flushItem("server_load", []any{"count", len(items)}, func() error {
		return w.store.AppendServerLoadPointsBulk(ctx, items)
	})
}

func (w *Writer) flushDCHealth(ctx context.Context, items []storage.DCHealthPointRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "dc_health", "count", len(items))
	w.flushItem("dc_health", []any{"count", len(items)}, func() error {
		return w.store.AppendDCHealthPointsBulk(ctx, items)
	})
}

func (w *Writer) flushClientIPs(ctx context.Context, items []storage.ClientIPHistoryRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "client_ips", "count", len(items))
	w.flushItem("client_ips", []any{"count", len(items)}, func() error {
		return w.store.UpsertClientIPHistoryBulk(ctx, items)
	})
}

// flushAuditEvents persists queued audit events as ONE multi-row insert
// (P6-6.1b, finding #10) via AppendAuditEventsBulk, replacing the previous
// per-row loop. Hash-chain integrity is unaffected: PrevHash/EventHash are
// assigned at enqueue time (audit_trail.go: chainAuditRecordLocked) and the
// bulk insert preserves slice order.
//
// A4 dead-letter semantics: audit is CRITICAL — a permanent store failure
// must not lose events. If the bulk insert ultimately fails, the WHOLE
// batch is spooled to the on-disk JSONL dead-letter file in order. This is
// coarser than the previous per-row spool (one poisoned row now drags its
// batch-mates into the spool instead of the DB) but nothing is ever
// silently dropped, and batches are small (audit flush threshold = 50).
func (w *Writer) flushAuditEvents(ctx context.Context, items []storage.AuditEventRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "audit", "count", len(items))
	persisted := w.flushItemTracked("audit", []any{"count", len(items)}, func() error {
		return w.store.AppendAuditEventsBulk(ctx, items)
	})
	if persisted {
		return
	}
	for _, item := range items {
		if err := w.writeDeadLetter(item); err != nil {
			slog.Error("audit dead-letter write failed",
				"domain", "audit",
				"audit_id", item.ID,
				"action", item.Action,
				"actor_id", item.ActorID,
				"error", err,
				"error_chain", errorChain(err),
				"alert", "audit_deadletter_write_failed",
			)
		}
	}
}

// auditDeadLetterFileName is the JSONL spool file name inside deadLetterDir.
// One append-only file keeps replay simple; rows are newline-delimited JSON of
// the storage.AuditEventRecord plus the time the event was dead-lettered.
const auditDeadLetterFileName = "audit-events.jsonl"

// deadLetteredAuditEvent is the on-disk JSONL envelope: the original audit
// record plus the wall-clock time it was spooled, so a later replay tool can
// order and de-duplicate entries.
type deadLetteredAuditEvent struct {
	DeadLetteredAt time.Time                `json:"dead_lettered_at"`
	Event          storage.AuditEventRecord `json:"event"`
}

// writeAuditDeadLetter appends one audit record to the dead-letter JSONL file,
// creating the directory and file on first use. It is the default
// w.writeDeadLetter implementation (A4). It opens with O_APPEND so concurrent
// writers do not clobber each other, though in practice only the single flush
// loop calls it.
func (w *Writer) writeAuditDeadLetter(item storage.AuditEventRecord) error {
	dir := w.deadLetterDir
	if dir == "" {
		dir = defaultAuditDeadLetterDir
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create audit dead-letter dir %q: %w", dir, err)
	}
	line, err := json.Marshal(deadLetteredAuditEvent{
		DeadLetteredAt: w.now().UTC(),
		Event:          item,
	})
	if err != nil {
		return fmt.Errorf("marshal audit dead-letter record %q: %w", item.ID, err)
	}
	path := filepath.Join(dir, auditDeadLetterFileName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640) //nolint:gosec // path is server-controlled, not user input
	if err != nil {
		return fmt.Errorf("open audit dead-letter file %q: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append audit dead-letter record %q: %w", item.ID, err)
	}
	return nil
}

// flushFallbackState persists queued put/delete ops against
// agent_fallback_state. The Store exposes single-row Put/Delete only, so we
// loop and route each op through flushItem to inherit the shared retry +
// classification + metrics observations. The in-memory s.fallback
// (agents.FallbackTracker) is the read source — this buffer exists only for durability
// across control-plane restarts.
func (w *Writer) flushFallbackState(ctx context.Context, items []fallbackStateOp) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "fallback_state", "count", len(items))
	for _, item := range items {
		item := item
		switch item.op {
		case "put":
			w.flushItem("fallback_state", []any{
				"agent_id", item.agentID,
				"op", "put",
			}, func() error {
				return w.store.PutAgentFallbackState(ctx, storage.AgentFallbackStateRecord{
					AgentID:   item.agentID,
					EnteredAt: item.enteredAt,
				})
			})
		case "delete":
			w.flushItem("fallback_state", []any{
				"agent_id", item.agentID,
				"op", "delete",
			}, func() error {
				return w.store.DeleteAgentFallbackState(ctx, item.agentID)
			})
		}
	}
}

// EnqueueFallbackPut queues a put against agent_fallback_state for the given
// agent and entered-at timestamp. Lock-free; safe to call under the server
// state mutex.
func (w *Writer) EnqueueFallbackPut(agentID string, enteredAt time.Time) {
	w.fallbackState.Enqueue(fallbackStateOp{agentID: agentID, enteredAt: enteredAt, op: "put"})
}

// EnqueueFallbackDelete queues a delete against agent_fallback_state for the
// given agent. Lock-free; safe to call under the server state mutex.
func (w *Writer) EnqueueFallbackDelete(agentID string) {
	w.fallbackState.Enqueue(fallbackStateOp{agentID: agentID, op: "delete"})
}

// FallbackOp is an exported view of one queued fallback-state operation.
type FallbackOp struct {
	AgentID   string
	EnteredAt time.Time
	Op        string // "put" or "delete"
}

// DrainPendingFallbackOps atomically removes and returns every fallback op
// currently queued in the fallback_state buffer WITHOUT flushing it to the
// store. It exists so callers in other packages (notably the server's
// fallback-transition tests) can assert the put/delete an agent-state
// transition enqueues, without reaching into this package's buffer internals.
func (w *Writer) DrainPendingFallbackOps() []FallbackOp {
	b := w.fallbackState
	b.mu.Lock()
	defer b.mu.Unlock()
	live := b.items[b.start:]
	ops := make([]FallbackOp, len(live))
	for i, op := range live {
		ops[i] = FallbackOp{AgentID: op.agentID, EnteredAt: op.enteredAt, Op: op.op}
	}
	b.items = b.items[:0]
	b.start = 0
	return ops
}

// flushTelemetry groups the batch by telemetry part and issues at most SIX
// bulk store calls per drain tick (P6-6.1a, finding #10) instead of up to
// 6×len(items) sequential single-row calls. Part semantics:
//
//   - runtime/diagnostics/security: nil pointer = no update; multiple units
//     for one agent collapse last-wins (bulk methods dedup by agent).
//   - dcs/upstreams: nil slice = no update, non-nil (incl. empty) = replace;
//     the LAST non-nil slice per agent in the batch wins.
//   - events: flat append; the (agent_id, sequence) upsert absorbs dups.
//
// Error semantics: one flushItem per PART (not per unit) — a persistent
// error drops that part for the whole batch and records a single
// ObservePersistRetry observation, matching the other bulk streams.
func (w *Writer) flushTelemetry(ctx context.Context, items []TelemetryWriteUnit) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "telemetry", "count", len(items))

	var runtimes []storage.TelemetryRuntimeCurrentRecord
	var dcsByAgent map[string][]storage.TelemetryRuntimeDCRecord
	var upstreamsByAgent map[string][]storage.TelemetryRuntimeUpstreamRecord
	var events []storage.TelemetryRuntimeEventRecord
	var diagnostics []storage.TelemetryDiagnosticsCurrentRecord
	var security []storage.TelemetrySecurityInventoryCurrentRecord

	for _, unit := range items {
		if unit.Runtime != nil {
			runtimes = append(runtimes, *unit.Runtime)
		}
		if unit.DCs != nil {
			if dcsByAgent == nil {
				dcsByAgent = make(map[string][]storage.TelemetryRuntimeDCRecord)
			}
			dcsByAgent[unit.AgentID] = unit.DCs
		}
		if unit.Upstreams != nil {
			if upstreamsByAgent == nil {
				upstreamsByAgent = make(map[string][]storage.TelemetryRuntimeUpstreamRecord)
			}
			upstreamsByAgent[unit.AgentID] = unit.Upstreams
		}
		events = append(events, unit.Events...)
		if unit.Diagnostics != nil {
			diagnostics = append(diagnostics, *unit.Diagnostics)
		}
		if unit.Security != nil {
			security = append(security, *unit.Security)
		}
	}

	if len(runtimes) > 0 {
		w.flushItem("telemetry", []any{"part", "runtime", "count", len(runtimes)}, func() error {
			return w.store.PutTelemetryRuntimeCurrentBulk(ctx, runtimes)
		})
	}
	if len(dcsByAgent) > 0 {
		w.flushItem("telemetry", []any{"part", "dcs", "agents", len(dcsByAgent)}, func() error {
			return w.store.ReplaceTelemetryRuntimeDCsBulk(ctx, dcsByAgent)
		})
	}
	if len(upstreamsByAgent) > 0 {
		w.flushItem("telemetry", []any{"part", "upstreams", "agents", len(upstreamsByAgent)}, func() error {
			return w.store.ReplaceTelemetryRuntimeUpstreamsBulk(ctx, upstreamsByAgent)
		})
	}
	if len(events) > 0 {
		w.flushItem("telemetry", []any{"part", "events", "count", len(events)}, func() error {
			return w.store.AppendTelemetryRuntimeEventsBulk(ctx, events)
		})
	}
	if len(diagnostics) > 0 {
		w.flushItem("telemetry", []any{"part", "diagnostics", "count", len(diagnostics)}, func() error {
			return w.store.PutTelemetryDiagnosticsCurrentBulk(ctx, diagnostics)
		})
	}
	if len(security) > 0 {
		w.flushItem("telemetry", []any{"part", "security", "count", len(security)}, func() error {
			return w.store.PutTelemetrySecurityInventoryCurrentBulk(ctx, security)
		})
	}
}
