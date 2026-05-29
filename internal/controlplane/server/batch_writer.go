package server

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
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

// batchMetricsSink is the narrow interface storeBatchWriter needs from the
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
type batchMetricsSink interface {
	ObserveFlushError(buffer, errorType string)
	SetQueueDepth(buffer string, depth float64)
	ObservePersistRetry(stream, outcome string)
	ObserveFlushDuration(stream string, seconds float64)
}

// batchBuffer accumulates items of type T and flushes them either on a timer
// or when the buffer reaches a size threshold. The flush function runs outside
// the buffer's own lock, so long-running DB writes do not block enqueue callers.
type batchBuffer[T any] struct {
	mu      sync.Mutex
	items   []T
	maxSize int
	flushFn func(ctx context.Context, items []T)
	signal  chan struct{}
}

func newBatchBuffer[T any](maxSize int, flush func(ctx context.Context, items []T)) *batchBuffer[T] {
	return &batchBuffer[T]{
		items:   make([]T, 0, maxSize),
		maxSize: maxSize,
		flushFn: flush,
		signal:  make(chan struct{}, 1),
	}
}

// Enqueue appends an item to the buffer. If the buffer is full, a non-blocking
// signal is sent to trigger an immediate flush.
func (b *batchBuffer[T]) Enqueue(item T) {
	b.mu.Lock()
	b.items = append(b.items, item)
	full := len(b.items) >= b.maxSize
	b.mu.Unlock()

	if full {
		select {
		case b.signal <- struct{}{}:
		default:
		}
	}
}

// Len returns the current number of queued items. Used by the flush loop to
// sample the queue-depth gauge before draining.
func (b *batchBuffer[T]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items)
}

// Drain swaps out the current buffer and flushes accumulated items. Safe to
// call concurrently — only one drain runs at a time because items are swapped
// under the lock before the flush function is invoked.
func (b *batchBuffer[T]) Drain(ctx context.Context) {
	b.mu.Lock()
	if len(b.items) == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.items
	b.items = make([]T, 0, b.maxSize)
	b.mu.Unlock()

	b.flushFn(ctx, batch)
}

// storeBatchWriter decouples storage I/O from the server's in-memory state
// mutex by accumulating writes in typed buffers and flushing them to the store
// on a background timer. This eliminates DB latency from the critical path.
type storeBatchWriter struct {
	store   storage.Store
	metrics batchMetricsSink
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

	agents     *batchBuffer[storage.AgentRecord]
	instances  *batchBuffer[storage.InstanceRecord]
	metricsBuf *batchBuffer[storage.MetricSnapshotRecord]
	serverLoad *batchBuffer[storage.ServerLoadPointRecord]
	dcHealth   *batchBuffer[storage.DCHealthPointRecord]
	clientIPs  *batchBuffer[storage.ClientIPHistoryRecord]
	telemetry  *batchBuffer[telemetryWriteUnit]
	// auditEvents is the 8th stream (P2-LOG-10 / M-R4 / P7-R6). Audit writes
	// used to run synchronously on the HTTP request path — a stalled DB froze
	// login and other handlers. Appending into this buffer is O(1) and the
	// background flush loop persists rows asynchronously with the shared
	// retry/classify logic. Critical-stream alerts are emitted via
	// streamAlerts[audit] so operators can page on persistent audit failures.
	auditEvents *batchBuffer[storage.AuditEventRecord]
	// fallbackState carries put/delete ops for agent_fallback_state. The
	// authoritative in-memory copy lives on Server.fallbackEnteredAt; this
	// buffer only persists transitions for restart durability.
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

// telemetryWriteUnit groups all per-agent telemetry writes for a single
// snapshot so they can be flushed together.
type telemetryWriteUnit struct {
	agentID     string
	runtime     *storage.TelemetryRuntimeCurrentRecord
	dcs         []storage.TelemetryRuntimeDCRecord
	upstreams   []storage.TelemetryRuntimeUpstreamRecord
	events      []storage.TelemetryRuntimeEventRecord
	diagnostics *storage.TelemetryDiagnosticsCurrentRecord
	security    *storage.TelemetrySecurityInventoryCurrentRecord
}

// noopMetricsSink is the default sink used when a caller does not supply one
// (e.g. legacy tests constructing the writer with nil metrics). It keeps the
// writer usable without the metrics subsystem wired in.
type noopMetricsSink struct{}

// noopMetricsSink implements batchMetricsSink as a null-object so the
// writer can run without a metrics subsystem wired in. Each method is
// intentionally a no-op — see the type comment above for context.
func (noopMetricsSink) ObserveFlushError(string, string)     { /* null-object */ }
func (noopMetricsSink) SetQueueDepth(string, float64)        { /* null-object */ }
func (noopMetricsSink) ObservePersistRetry(string, string)   { /* null-object */ }
func (noopMetricsSink) ObserveFlushDuration(string, float64) { /* null-object */ }

func newStoreBatchWriter(store storage.Store, metrics batchMetricsSink, now func() time.Time) *storeBatchWriter {
	if metrics == nil {
		metrics = noopMetricsSink{}
	}
	if now == nil {
		now = time.Now
	}
	w := &storeBatchWriter{
		store:         store,
		metrics:       metrics,
		done:          make(chan struct{}),
		flushInterval: batchFlushInterval,
		sleep:         time.Sleep,
		now:           now,
	}

	w.agents = newBatchBuffer(batchSizeFor("agents"), w.flushAgents)
	w.instances = newBatchBuffer(batchSizeFor("instances"), w.flushInstances)
	w.metricsBuf = newBatchBuffer(batchSizeFor("metrics"), w.flushMetrics)
	w.serverLoad = newBatchBuffer(batchSizeFor("server_load"), w.flushServerLoad)
	w.dcHealth = newBatchBuffer(batchSizeFor("dc_health"), w.flushDCHealth)
	w.clientIPs = newBatchBuffer(batchSizeFor("client_ips"), w.flushClientIPs)
	w.telemetry = newBatchBuffer(batchSizeFor("telemetry"), w.flushTelemetry)
	w.auditEvents = newBatchBuffer(batchSizeFor("audit"), w.flushAuditEvents)
	w.fallbackState = newBatchBuffer(batchSizeFor("fallback_state"), w.flushFallbackState)

	return w
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
func (w *storeBatchWriter) Start(parentCtx context.Context) {
	if parentCtx == nil {
		parentCtx = context.WithoutCancel(context.Background()) //nolint:noctx,contextcheck // reason: test-only fallback when Start is called without a lifecycle ctx; production wiring (lifecycle.go) always supplies serverCtx.
	}
	w.wg.Add(1)
	go w.flushLoop(parentCtx)
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
func (w *storeBatchWriter) StopWithTimeout(parentCtx context.Context, timeout time.Duration) error {
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
		// block forever because classifyFlushError + retry will bail on
		// context.DeadlineExceeded.
		<-done
		return ctx.Err()
	}
}

func (w *storeBatchWriter) flushLoop(parentCtx context.Context) {
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
		case <-w.fallbackState.signal:
		}
		// Any signal (including ticker) triggers a full drain of all buffers.
		// This prevents signal coalescing from letting individual buffers grow
		// beyond its per-stream batch size for more than one flush cycle.
		w.drainAll(flushCtx)
	}
}

// observeQueueDepths samples every buffer's pending count and publishes it to
// the Prometheus gauge. Called BEFORE drain so the gauge reflects the peak
// queue depth observed by Prometheus (post-drain the value would always be
// zero and useless for alerting).
func (w *storeBatchWriter) observeQueueDepths() {
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

func (w *storeBatchWriter) drainAll(ctx context.Context) {
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
	w.fallbackState.Drain(ctx)
}

// classifyFlushError returns "transient" or "persistent" for an error produced
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
func classifyFlushError(err error) string {
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
func (w *storeBatchWriter) retryWithBackoff(op func() error, observeTransient func()) (retryOutcome, error) {
	err := op()
	if err == nil {
		return retrySucceededFirstTry, nil
	}
	if classifyFlushError(err) == "persistent" {
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
		if classifyFlushError(err) == "persistent" {
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
// streamAlerts is the set of streams that warrant a "critical alert" marker
// on the error log line — currently reserved for the audit stream once it
// moves into the batch writer (see P2-LOG-10). For now no buffer is in this
// set, but the hook is here so the plumbing exists and only the list needs
// updating when audit async writes land.
var streamAlerts = map[string]string{
	// P2-LOG-10 / M-R4 / P7-R6: audit is the CRITICAL stream. Persistent
	// failures (NOT NULL violation, schema mismatch, retry-exhausted
	// transient) emit slog.Error with alert=audit_persist_failed so
	// operator paging rules can match a single stable key.
	"audit": "audit_persist_failed",
	// fallback_state is a CRITICAL stream too: a missed put/delete here
	// silently drifts the in-memory fallbackEnteredAt map from the
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

func (w *storeBatchWriter) flushItem(buffer string, logAttrs []any, op func() error) {
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
		return
	case retrySucceededOnRetry:
		// Retry saved us — record the positive outcome but do not log.
		w.metrics.ObservePersistRetry(buffer, "success")
		return
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
	if alert, ok := streamAlerts[buffer]; ok {
		attrs = append(attrs, "alert", alert)
	}
	slog.Error("batch persist failed", attrs...)
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

func (w *storeBatchWriter) flushAgents(ctx context.Context, items []storage.AgentRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "agents", "count", len(items))
	w.flushItem("agents", []any{"count", len(items)}, func() error {
		return w.store.PutAgentsBulk(ctx, items)
	})
}

func (w *storeBatchWriter) flushInstances(ctx context.Context, items []storage.InstanceRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "instances", "count", len(items))
	w.flushItem("instances", []any{"count", len(items)}, func() error {
		return w.store.PutInstancesBulk(ctx, items)
	})
}

func (w *storeBatchWriter) flushMetrics(ctx context.Context, items []storage.MetricSnapshotRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "metrics", "count", len(items))
	w.flushItem("metrics", []any{"count", len(items)}, func() error {
		return w.store.AppendMetricSnapshotsBulk(ctx, items)
	})
}

func (w *storeBatchWriter) flushServerLoad(ctx context.Context, items []storage.ServerLoadPointRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "server_load", "count", len(items))
	w.flushItem("server_load", []any{"count", len(items)}, func() error {
		return w.store.AppendServerLoadPointsBulk(ctx, items)
	})
}

func (w *storeBatchWriter) flushDCHealth(ctx context.Context, items []storage.DCHealthPointRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "dc_health", "count", len(items))
	w.flushItem("dc_health", []any{"count", len(items)}, func() error {
		return w.store.AppendDCHealthPointsBulk(ctx, items)
	})
}

func (w *storeBatchWriter) flushClientIPs(ctx context.Context, items []storage.ClientIPHistoryRecord) {
	if len(items) == 0 {
		return
	}
	slog.Debug(logBatchFlush, "domain", "client_ips", "count", len(items))
	w.flushItem("client_ips", []any{"count", len(items)}, func() error {
		return w.store.UpsertClientIPHistoryBulk(ctx, items)
	})
}

// flushAuditEvents persists queued audit events one row at a time through the
// existing single-row Store API (AppendAuditEvent). The Store interface does
// not expose a batch AppendAuditEvents method today, so we loop — per
// P2-LOG-10 scoping: "Prefer batch API if store supports it; else reuse
// existing single-insert." A batch API could be added later as a pure
// performance optimisation without changing this call site's contract.
//
// Each row goes through flushItem so it inherits the shared retry +
// classification + metrics observations, and — crucially for P7-R6 — the
// audit-specific alert key ("alert=audit_persist_failed") surfaces via
// streamAlerts[audit] when a row cannot be persisted.
func (w *storeBatchWriter) flushAuditEvents(ctx context.Context, items []storage.AuditEventRecord) {
	slog.Debug(logBatchFlush, "domain", "audit", "count", len(items))
	for _, item := range items {
		item := item
		w.flushItem("audit", []any{
			"audit_id", item.ID,
			"action", item.Action,
			"actor_id", item.ActorID,
		}, func() error {
			return w.store.AppendAuditEvent(ctx, item)
		})
	}
}

// flushFallbackState persists queued put/delete ops against
// agent_fallback_state. The Store exposes single-row Put/Delete only, so we
// loop and route each op through flushItem to inherit the shared retry +
// classification + metrics observations. The in-memory fallbackEnteredAt map
// on Server is the read source — this buffer exists only for durability
// across control-plane restarts.
func (w *storeBatchWriter) flushFallbackState(ctx context.Context, items []fallbackStateOp) {
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
func (w *storeBatchWriter) EnqueueFallbackPut(agentID string, enteredAt time.Time) {
	w.fallbackState.Enqueue(fallbackStateOp{agentID: agentID, enteredAt: enteredAt, op: "put"})
}

// EnqueueFallbackDelete queues a delete against agent_fallback_state for the
// given agent. Lock-free; safe to call under the server state mutex.
func (w *storeBatchWriter) EnqueueFallbackDelete(agentID string) {
	w.fallbackState.Enqueue(fallbackStateOp{agentID: agentID, op: "delete"})
}

func (w *storeBatchWriter) flushTelemetry(ctx context.Context, items []telemetryWriteUnit) {
	slog.Debug(logBatchFlush, "domain", "telemetry", "count", len(items))
	for _, unit := range items {
		unit := unit
		if unit.runtime != nil {
			w.flushItem("telemetry", []any{"part", "runtime", "agent_id", unit.agentID}, func() error {
				return w.store.PutTelemetryRuntimeCurrent(ctx, *unit.runtime)
			})
		}
		if unit.dcs != nil {
			w.flushItem("telemetry", []any{"part", "dcs", "agent_id", unit.agentID}, func() error {
				return w.store.ReplaceTelemetryRuntimeDCs(ctx, unit.agentID, unit.dcs)
			})
		}
		if unit.upstreams != nil {
			w.flushItem("telemetry", []any{"part", "upstreams", "agent_id", unit.agentID}, func() error {
				return w.store.ReplaceTelemetryRuntimeUpstreams(ctx, unit.agentID, unit.upstreams)
			})
		}
		if unit.events != nil {
			w.flushItem("telemetry", []any{"part", "events", "agent_id", unit.agentID}, func() error {
				return w.store.AppendTelemetryRuntimeEvents(ctx, unit.agentID, unit.events)
			})
		}
		if unit.diagnostics != nil {
			w.flushItem("telemetry", []any{"part", "diagnostics", "agent_id", unit.agentID}, func() error {
				return w.store.PutTelemetryDiagnosticsCurrent(ctx, *unit.diagnostics)
			})
		}
		if unit.security != nil {
			w.flushItem("telemetry", []any{"part", "security", "agent_id", unit.agentID}, func() error {
				return w.store.PutTelemetrySecurityInventoryCurrent(ctx, *unit.security)
			})
		}
	}
}
