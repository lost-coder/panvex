package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

const (
	batchFlushInterval = 500 * time.Millisecond
	batchMaxSize       = 50
)

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
	store  storage.Store
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	audit      *batchBuffer[storage.AuditEventRecord]
	agents     *batchBuffer[storage.AgentRecord]
	instances  *batchBuffer[storage.InstanceRecord]
	metrics    *batchBuffer[storage.MetricSnapshotRecord]
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

func newStoreBatchWriter(store storage.Store) *storeBatchWriter {
	ctx, cancel := context.WithCancel(context.Background())
	w := &storeBatchWriter{
		store:  store,
		ctx:    ctx,
		cancel: cancel,
	}

	w.audit = newBatchBuffer(batchMaxSize, w.flushAudit)
	w.agents = newBatchBuffer(batchMaxSize, w.flushAgents)
	w.instances = newBatchBuffer(batchMaxSize, w.flushInstances)
	w.metrics = newBatchBuffer(batchMaxSize, w.flushMetrics)
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
			w.drainAll(w.ctx)
		case <-w.audit.signal:
			w.audit.Drain(w.ctx)
		case <-w.agents.signal:
			w.agents.Drain(w.ctx)
		case <-w.instances.signal:
			w.instances.Drain(w.ctx)
		case <-w.metrics.signal:
			w.metrics.Drain(w.ctx)
		case <-w.serverLoad.signal:
			w.serverLoad.Drain(w.ctx)
		case <-w.dcHealth.signal:
			w.dcHealth.Drain(w.ctx)
		case <-w.clientIPs.signal:
			w.clientIPs.Drain(w.ctx)
		case <-w.telemetry.signal:
			w.telemetry.Drain(w.ctx)
		}
	}
}

func (w *storeBatchWriter) drainAll(ctx context.Context) {
	w.audit.Drain(ctx)
	w.agents.Drain(ctx)
	w.instances.Drain(ctx)
	w.metrics.Drain(ctx)
	w.serverLoad.Drain(ctx)
	w.dcHealth.Drain(ctx)
	w.clientIPs.Drain(ctx)
	w.telemetry.Drain(ctx)
}

// Flush functions — each iterates accumulated items and calls the
// corresponding Store method. Errors are logged but not propagated because
// the in-memory state is already committed and the next snapshot will
// overwrite stale DB rows.

func (w *storeBatchWriter) flushAudit(ctx context.Context, items []storage.AuditEventRecord) {
	slog.Debug("batch flush", "domain", "audit", "count", len(items))
	for _, item := range items {
		if err := w.store.AppendAuditEvent(ctx, item); err != nil {
			slog.Warn("batch persist failed", "domain", "audit", "error", err)
		}
	}
}

func (w *storeBatchWriter) flushAgents(ctx context.Context, items []storage.AgentRecord) {
	slog.Debug("batch flush", "domain", "agents", "count", len(items))
	for _, item := range items {
		if err := w.store.PutAgent(ctx, item); err != nil {
			slog.Warn("batch persist failed", "domain", "agents", "agent_id", item.ID, "error", err)
		}
	}
}

func (w *storeBatchWriter) flushInstances(ctx context.Context, items []storage.InstanceRecord) {
	slog.Debug("batch flush", "domain", "instances", "count", len(items))
	for _, item := range items {
		if err := w.store.PutInstance(ctx, item); err != nil {
			slog.Warn("batch persist failed", "domain", "instances", "instance_id", item.ID, "error", err)
		}
	}
}

func (w *storeBatchWriter) flushMetrics(ctx context.Context, items []storage.MetricSnapshotRecord) {
	slog.Debug("batch flush", "domain", "metrics", "count", len(items))
	for _, item := range items {
		if err := w.store.AppendMetricSnapshot(ctx, item); err != nil {
			slog.Warn("batch persist failed", "domain", "metrics", "error", err)
		}
	}
}

func (w *storeBatchWriter) flushServerLoad(ctx context.Context, items []storage.ServerLoadPointRecord) {
	slog.Debug("batch flush", "domain", "server_load", "count", len(items))
	for _, item := range items {
		if err := w.store.AppendServerLoadPoint(ctx, item); err != nil {
			slog.Warn("batch persist failed", "domain", "server_load", "agent_id", item.AgentID, "error", err)
		}
	}
}

func (w *storeBatchWriter) flushDCHealth(ctx context.Context, items []storage.DCHealthPointRecord) {
	slog.Debug("batch flush", "domain", "dc_health", "count", len(items))
	for _, item := range items {
		if err := w.store.AppendDCHealthPoint(ctx, item); err != nil {
			slog.Warn("batch persist failed", "domain", "dc_health", "error", err)
		}
	}
}

func (w *storeBatchWriter) flushClientIPs(ctx context.Context, items []storage.ClientIPHistoryRecord) {
	slog.Debug("batch flush", "domain", "client_ips", "count", len(items))
	for _, item := range items {
		if err := w.store.UpsertClientIPHistory(ctx, item); err != nil {
			slog.Warn("batch persist failed", "domain", "client_ips", "error", err)
		}
	}
}

func (w *storeBatchWriter) flushTelemetry(ctx context.Context, items []telemetryWriteUnit) {
	slog.Debug("batch flush", "domain", "telemetry", "count", len(items))
	for _, unit := range items {
		if unit.runtime != nil {
			if err := w.store.PutTelemetryRuntimeCurrent(ctx, *unit.runtime); err != nil {
				slog.Warn("batch persist failed", "domain", "telemetry_runtime", "agent_id", unit.agentID, "error", err)
				continue
			}
		}
		if unit.dcs != nil {
			if err := w.store.ReplaceTelemetryRuntimeDCs(ctx, unit.agentID, unit.dcs); err != nil {
				slog.Warn("batch persist failed", "domain", "telemetry_dcs", "agent_id", unit.agentID, "error", err)
			}
		}
		if unit.upstreams != nil {
			if err := w.store.ReplaceTelemetryRuntimeUpstreams(ctx, unit.agentID, unit.upstreams); err != nil {
				slog.Warn("batch persist failed", "domain", "telemetry_upstreams", "agent_id", unit.agentID, "error", err)
			}
		}
		if unit.events != nil {
			if err := w.store.AppendTelemetryRuntimeEvents(ctx, unit.agentID, unit.events); err != nil {
				slog.Warn("batch persist failed", "domain", "telemetry_events", "agent_id", unit.agentID, "error", err)
			}
		}
		if unit.diagnostics != nil {
			if err := w.store.PutTelemetryDiagnosticsCurrent(ctx, *unit.diagnostics); err != nil {
				slog.Warn("batch persist failed", "domain", "telemetry_diagnostics", "agent_id", unit.agentID, "error", err)
			}
		}
		if unit.security != nil {
			if err := w.store.PutTelemetrySecurityInventoryCurrent(ctx, *unit.security); err != nil {
				slog.Warn("batch persist failed", "domain", "telemetry_security", "agent_id", unit.agentID, "error", err)
			}
		}
	}
}
