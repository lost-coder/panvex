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
	batchMaxSize       = 50
)

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
type batchMetricsSink interface {
	ObserveFlushError(buffer, errorType string)
	SetQueueDepth(buffer string, depth float64)
	ObservePersistRetry(stream, outcome string)
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
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// sleep is the backoff sleeper used by retryWithBackoff. Exported through
	// a field so tests can override it to zero-duration sleeps without
	// blocking the test for seconds.
	sleep func(d time.Duration)

	agents     *batchBuffer[storage.AgentRecord]
	instances  *batchBuffer[storage.InstanceRecord]
	metricsBuf *batchBuffer[storage.MetricSnapshotRecord]
	serverLoad *batchBuffer[storage.ServerLoadPointRecord]
	dcHealth   *batchBuffer[storage.DCHealthPointRecord]
	clientIPs  *batchBuffer[storage.ClientIPHistoryRecord]
	telemetry  *batchBuffer[telemetryWriteUnit]
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

func (noopMetricsSink) ObserveFlushError(string, string)   {}
func (noopMetricsSink) SetQueueDepth(string, float64)      {}
func (noopMetricsSink) ObservePersistRetry(string, string) {}

func newStoreBatchWriter(store storage.Store, metrics batchMetricsSink) *storeBatchWriter {
	if metrics == nil {
		metrics = noopMetricsSink{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &storeBatchWriter{
		store:   store,
		metrics: metrics,
		ctx:     ctx,
		cancel:  cancel,
		sleep:   time.Sleep,
	}

	w.agents = newBatchBuffer(batchMaxSize, w.flushAgents)
	w.instances = newBatchBuffer(batchMaxSize, w.flushInstances)
	w.metricsBuf = newBatchBuffer(batchMaxSize, w.flushMetrics)
	w.serverLoad = newBatchBuffer(batchMaxSize, w.flushServerLoad)
	w.dcHealth = newBatchBuffer(batchMaxSize, w.flushDCHealth)
	w.clientIPs = newBatchBuffer(batchMaxSize, w.flushClientIPs)
	w.telemetry = newBatchBuffer(batchMaxSize, w.flushTelemetry)

	return w
}

// Start launches the background flush goroutine.
func (w *storeBatchWriter) Start() {
	w.wg.Add(1)
	go w.flushLoop()
}

// Stop cancels the background goroutine, waits for it to exit, then performs
// a final drain of all buffers so pending writes are persisted before the
// store is closed.
func (w *storeBatchWriter) Stop() {
	w.cancel()
	w.wg.Wait()
	w.drainAll(context.Background())
}

func (w *storeBatchWriter) flushLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(batchFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
		case <-w.agents.signal:
		case <-w.instances.signal:
		case <-w.metricsBuf.signal:
		case <-w.serverLoad.signal:
		case <-w.dcHealth.signal:
		case <-w.clientIPs.signal:
		case <-w.telemetry.signal:
		}
		// Any signal (including ticker) triggers a full drain of all buffers.
		// This prevents signal coalescing from letting individual buffers grow
		// beyond batchMaxSize for more than one flush cycle.
		w.drainAll(context.Background())
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
// op returns a transient error. It returns the final error (nil on success)
// and a retryOutcome describing how the sequence terminated.
//
// Each failed attempt increments the per-stream transient counter via
// observeTransient. The final outcome is reported separately by the caller
// using ObservePersistRetry so operators can distinguish "eventually
// succeeded" from "gave up and dropped".
func (w *storeBatchWriter) retryWithBackoff(op func() error, observeTransient func()) (error, retryOutcome) {
	err := op()
	if err == nil {
		return nil, retrySucceededFirstTry
	}
	if classifyFlushError(err) == "persistent" {
		return err, retryPersistent
	}

	for _, delay := range retryBackoffs {
		// Record the failed-and-retrying attempt as a transient observation.
		if observeTransient != nil {
			observeTransient()
		}
		// Abort retry loop if the writer is shutting down — no point sleeping
		// for hundreds of ms when the process is exiting.
		select {
		case <-w.ctx.Done():
			return err, retryExhausted
		default:
		}
		w.sleep(delay)
		err = op()
		if err == nil {
			return nil, retrySucceededOnRetry
		}
		if classifyFlushError(err) == "persistent" {
			return err, retryPersistent
		}
	}
	// All attempts burned and the last error was still transient.
	if observeTransient != nil {
		observeTransient()
	}
	return err, retryExhausted
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
	// "audit": "audit_persist_failed",
}

func (w *storeBatchWriter) flushItem(buffer string, logAttrs []any, op func() error) {
	err, outcome := w.retryWithBackoff(op, func() {
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

// Flush functions — each iterates accumulated items and calls the
// corresponding Store method through flushItem, which handles retry,
// classification, logging, and metric observation.

func (w *storeBatchWriter) flushAgents(ctx context.Context, items []storage.AgentRecord) {
	slog.Debug("batch flush", "domain", "agents", "count", len(items))
	for _, item := range items {
		item := item
		w.flushItem("agents", []any{"agent_id", item.ID}, func() error {
			return w.store.PutAgent(ctx, item)
		})
	}
}

func (w *storeBatchWriter) flushInstances(ctx context.Context, items []storage.InstanceRecord) {
	slog.Debug("batch flush", "domain", "instances", "count", len(items))
	for _, item := range items {
		item := item
		w.flushItem("instances", []any{"instance_id", item.ID}, func() error {
			return w.store.PutInstance(ctx, item)
		})
	}
}

func (w *storeBatchWriter) flushMetrics(ctx context.Context, items []storage.MetricSnapshotRecord) {
	slog.Debug("batch flush", "domain", "metrics", "count", len(items))
	for _, item := range items {
		item := item
		w.flushItem("metrics", nil, func() error {
			return w.store.AppendMetricSnapshot(ctx, item)
		})
	}
}

func (w *storeBatchWriter) flushServerLoad(ctx context.Context, items []storage.ServerLoadPointRecord) {
	slog.Debug("batch flush", "domain", "server_load", "count", len(items))
	for _, item := range items {
		item := item
		w.flushItem("server_load", []any{"agent_id", item.AgentID}, func() error {
			return w.store.AppendServerLoadPoint(ctx, item)
		})
	}
}

func (w *storeBatchWriter) flushDCHealth(ctx context.Context, items []storage.DCHealthPointRecord) {
	slog.Debug("batch flush", "domain", "dc_health", "count", len(items))
	for _, item := range items {
		item := item
		w.flushItem("dc_health", nil, func() error {
			return w.store.AppendDCHealthPoint(ctx, item)
		})
	}
}

func (w *storeBatchWriter) flushClientIPs(ctx context.Context, items []storage.ClientIPHistoryRecord) {
	slog.Debug("batch flush", "domain", "client_ips", "count", len(items))
	for _, item := range items {
		item := item
		w.flushItem("client_ips", nil, func() error {
			return w.store.UpsertClientIPHistory(ctx, item)
		})
	}
}

func (w *storeBatchWriter) flushTelemetry(ctx context.Context, items []telemetryWriteUnit) {
	slog.Debug("batch flush", "domain", "telemetry", "count", len(items))
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
