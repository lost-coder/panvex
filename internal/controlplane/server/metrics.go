package server

import (
	"bufio"
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metricsCollectors bundles the Prometheus collectors exposed at /metrics for
// scrape by an external Prometheus server or compatible agent.
//
// Cardinality safety
// ------------------
// Label values MUST come from a small, bounded set. In particular:
//   - DO NOT label any metric with a UserID, agent ID, client ID, instance ID,
//     session ID, IP address, or raw request URL. These are unbounded (or
//     attacker-controlled) inputs and will blow up series cardinality.
//   - The HTTP middleware uses chi's registered route pattern (e.g.
//     "/api/clients/{id}"), NOT r.URL.Path, so that two requests for
//     different IDs fold into one series.
//   - The only safe label values are the explicit enums listed next to each
//     metric below (buffer name, error_type transient/persistent, http
//     method/path-pattern/status-bucket).
//
// Downstream tasks (P2-OBS-03, P2-REL-06, P2-LOG-10, P2-PERF-05, P3-OBS-01)
// will wire their specific counters using the accessor methods on *Server —
// this task only registers collectors and wires the "obvious" ones: HTTP
// request metrics, event-hub drops, and the agent-connected gauge.
type metricsCollectors struct {
	registry *prometheus.Registry

	httpRequestDuration *prometheus.HistogramVec
	httpRequestsTotal   *prometheus.CounterVec

	agentConnected prometheus.Gauge

	batchQueueDepth       *prometheus.GaugeVec
	batchFlushErrorsTotal *prometheus.CounterVec
	// P2-REL-06: batch_writer retry + persistence error surfacing (H14).
	// persist_errors_total mirrors flush_errors_total with the spec-mandated
	// label names (stream/type instead of buffer/error_type). The older metric
	// is kept in place so dashboards from P2-OBS-01 keep working.
	batchPersistErrorsTotal  *prometheus.CounterVec
	batchPersistRetriesTotal *prometheus.CounterVec
	// P2-OBS-03: per-stream flush latency histogram (including retries).
	batchFlushDuration *prometheus.HistogramVec

	eventHubDropTotal   prometheus.Counter
	eventHubSubscribers prometheus.Gauge

	// D-2: silent-drop visibility for regular-priority inbound agent
	// messages. enqueueInboundAgentMessage uses drop-oldest semantics under
	// backpressure, but the rare "all three non-blocking attempts lost"
	// case (drained slot snatched by a concurrent reader) used to vanish
	// silently. Bumped from the regular-path drop branch only — priority
	// messages still block and never feed this counter. Intentionally
	// label-less to honour the cardinality rule above (no agent_id).
	agentInboundDropsTotal prometheus.Counter

	jobQueueDepth prometheus.Gauge
	lockoutActive prometheus.Gauge

	// C3: write-behind job persist failures. The jobs service retries on
	// the next mutation, but a wedged DB used to be slog-only; this
	// counter backs the PanvexJobPersistFailures alert.
	jobPersistFailuresTotal prometheus.Counter

	unsignedUpdateFallbackTotal prometheus.Counter

	// P2-REL-04 / P2-REL-05: per-table row count deleted by the retention
	// worker. Labels are a bounded enum (see retentionPruneTables below) so
	// cardinality stays safe.
	retentionPrunedRowsTotal *prometheus.CounterVec

	// Q3.U-Q-15: per-goroutine panic-recovery counter so a silently
	// recovered panic surfaces as a Prometheus alert instead of vanishing
	// into a single log line. Labels: goroutine name (bounded enum from
	// the call sites — receive, priority-inbound, audit-effects, etc).
	panicRecoveredTotal *prometheus.CounterVec

	// Phase-2 §2.1: connection pool visibility. Driven by a periodic
	// publisher goroutine that snapshots store.PoolStats() onto these
	// gauges every 15s. PromQL alert thresholds live in
	// deploy/prometheus/alerts.yaml.
	dbPoolOpen          prometheus.Gauge // currently open connections (in_use + idle)
	dbPoolInUse         prometheus.Gauge // connections actively serving a query
	dbPoolIdle          prometheus.Gauge // idle connections retained in the pool
	dbPoolMaxOpen       prometheus.Gauge // configured upper limit (snapshot)
	dbPoolWaitTotal     prometheus.Counter
	dbPoolWaitSeconds   prometheus.Counter
	dbPoolMaxIdleClosed prometheus.Counter
	dbPoolLifetimeClose prometheus.Counter

	// Phase-2 §2.1: rate-limit rejections by scope. Lets oncall see at
	// a glance whether a flood is hitting login, the agent bootstrap,
	// or the per-user sensitive bucket.
	rateLimitRejectedTotal *prometheus.CounterVec

	// Reverse-mode transport metrics (Task 17).
	// outboundSupervisorsTotal tracks how many outbound (reverse-mode)
	// supervisors are currently running, labelled by transport mode.
	// Label values: "outbound". Pre-initialised to zero so dashboards see
	// the series even before the first reverse agent is enrolled.
	outboundSupervisorsTotal *prometheus.GaugeVec
	// bootstrapAttemptsTotal counts EnrollDriver.Run outcomes, labelled by
	// result. Bounded label enum: success|expired|mismatch|agent_id_mismatch|
	// misbehavior|error. Pre-initialised to zero for alert stability.
	bootstrapAttemptsTotal *prometheus.CounterVec
	// agentCertPinTotal counts dial-time SPKI pin verification outcomes,
	// labelled by result. Bounded enum: ok|mismatch|missing.
	// Pre-initialised to zero for PromQL alert stability. (S-02)
	agentCertPinTotal *prometheus.CounterVec

	// F3 (audit 2026-06-09): certificate expiry surfaced as unix
	// timestamps so PromQL can alert on `x - time() < threshold`.
	// 0 means "not yet sampled / no data" — alert rules must guard
	// with `> 0`.
	caCertExpiryTimestamp            prometheus.Gauge
	serverCertExpiryTimestamp        prometheus.Gauge
	agentCertEarliestExpiryTimestamp prometheus.Gauge
}

// rateLimitScopes enumerates every scope label that can appear on
// panvex_ratelimit_rejected_total. Pre-initialised to zero at startup
// (keeps PromQL alerts deterministic).
var rateLimitScopes = []string{"login", "agent_bootstrap", "sensitive", "grpc_connect"}

// retentionPruneTables enumerates every table whose retention worker feeds
// panvex_retention_pruned_rows_total. Adding a new retention worker requires
// adding the table name here so the series is pre-initialised to zero at
// startup (keeps PromQL alerts deterministic; see H-style alerting).
var retentionPruneTables = []string{
	"audit_events",
	"metric_snapshots",
	"jobs",
	"webhook_outbox",
	"agent_revocations",
	"enrollment_tokens",
}

// knownBatchBuffers enumerates every batch buffer tracked by
// panvex_batch_queue_depth and panvex_batch_flush_errors_total. Adding a new
// buffer requires adding its name here so the gauge series is pre-initialised
// to zero at startup (makes PromQL alerts deterministic).
var knownBatchBuffers = []string{
	"agents",
	"instances",
	"metrics",
	"server_load",
	"dc_health",
	"client_ips",
	"telemetry",
}

// newMetricsCollectors constructs and registers all Prometheus collectors
// owned by the control-plane server. Each *Server gets its own registry so
// tests do not fight over the global default registry.
func newMetricsCollectors() *metricsCollectors {
	reg := prometheus.NewRegistry()

	mc := &metricsCollectors{
		registry: reg,
		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "panvex_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds, bucketed by method, route pattern, and status bucket.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path", "status"}),
		httpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_http_requests_total",
			Help: "Total number of HTTP requests handled, labelled by method, route pattern, and status bucket.",
		}, []string{"method", "path", "status"}),
		agentConnected: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_agent_connected",
			Help: "Number of agents currently evaluated as connected (presence state is not offline).",
		}),
		batchQueueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "panvex_batch_queue_depth",
			Help: "Number of items queued in each batch writer buffer, waiting to be flushed to storage.",
		}, []string{"buffer"}),
		batchFlushErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_batch_flush_errors_total",
			Help: "Total number of batch flush errors by buffer and error_type (transient|persistent).",
		}, []string{"buffer", "error_type"}),
		batchPersistErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_batch_persist_errors_total",
			Help: "Batch writer persistence errors by stream and type (transient|persistent). A transient increment means an individual retry attempt failed; the persistent counter increments once per item that was ultimately dropped.",
		}, []string{"stream", "type"}),
		batchPersistRetriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_batch_persist_retries_total",
			Help: "Batch writer retry outcomes by stream and outcome (success|exhausted). Success means a retry eventually succeeded; exhausted means all retries were used up and the item was dropped.",
		}, []string{"stream", "outcome"}),
		batchFlushDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "panvex_batch_flush_duration_seconds",
			Help: "Per-stream batch flush latency in seconds, measured end-to-end including all retries, regardless of outcome.",
			// Buckets tuned for DB flush latency: sub-millisecond work on a
			// hot local SQLite path up to multi-second tails when retries
			// burn the full backoff schedule on a flaky upstream.
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"stream"}),
		eventHubDropTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_event_hub_drop_total",
			Help: "Total number of events dropped because a subscriber channel was full.",
		}),
		agentInboundDropsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_agent_inbound_drops_total",
			Help: "Inbound agent messages dropped on the regular-priority queue when the drop-oldest retry races with a concurrent reader. Intentionally label-less to keep cardinality bounded (D-2).",
		}),
		eventHubSubscribers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_event_hub_subscribers",
			Help: "Current number of event-hub subscribers.",
		}),
		jobQueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_job_queue_depth",
			Help: "Current number of jobs in the queued/running state.",
		}),
		lockoutActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_lockout_active",
			Help: "Current number of usernames with an active account lockout.",
		}),
		jobPersistFailuresTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_job_persist_failures_total",
			Help: "Total job write-behind persistence failures (PutJob/PutJobTarget). In-memory state stays ahead of storage until the next successful persist; sustained growth means job state is not durable.",
		}),
		unsignedUpdateFallbackTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_unsigned_update_fallback_total",
			Help: "Total number of panel-update applications that fell back to an unsigned manifest.",
		}),
		retentionPrunedRowsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_retention_pruned_rows_total",
			Help: "Total rows deleted by the retention worker, labelled by table. Bounded enum: audit_events, metric_snapshots, jobs, webhook_outbox.",
		}, []string{"table"}),
		panicRecoveredTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_goroutine_panic_recovered_total",
			Help: "Total panics caught by recoverGoroutine, labelled by goroutine name (Q3.U-Q-15).",
		}, []string{"goroutine"}),
		dbPoolOpen: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_db_pool_open_connections",
			Help: "Current number of open database connections (in_use + idle). Sample period: 15s.",
		}),
		dbPoolInUse: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_db_pool_in_use_connections",
			Help: "Number of database connections currently serving a query.",
		}),
		dbPoolIdle: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_db_pool_idle_connections",
			Help: "Number of idle database connections retained in the pool.",
		}),
		dbPoolMaxOpen: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_db_pool_max_open_connections",
			Help: "Configured upper limit on simultaneous open connections (PANVEX_DB_MAX_OPEN_CONNS).",
		}),
		dbPoolWaitTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_db_pool_wait_total",
			Help: "Total number of times a goroutine had to wait for an available connection.",
		}),
		dbPoolWaitSeconds: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_db_pool_wait_seconds_total",
			Help: "Cumulative time goroutines spent waiting for a free connection, in seconds.",
		}),
		dbPoolMaxIdleClosed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_db_pool_max_idle_closed_total",
			Help: "Total connections closed because MaxIdleConns was exceeded.",
		}),
		dbPoolLifetimeClose: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_db_pool_lifetime_closed_total",
			Help: "Total connections closed because ConnMaxLifetime was exceeded.",
		}),
		rateLimitRejectedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_ratelimit_rejected_total",
			Help: "Total rate-limit rejections, labelled by scope. Bounded enum.",
		}, []string{"scope"}),
		outboundSupervisorsTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "panvex_outbound_supervisors_total",
			Help: "Number of outbound (reverse-mode) supervisors currently active, labelled by mode. Incremented by ensureSupervisor, decremented by removeSupervisor/stopAll.",
		}, []string{"mode"}),
		bootstrapAttemptsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_bootstrap_attempts_total",
			Help: "Reverse-mode bootstrap enrollment attempts by result. Bounded label enum: success|expired|mismatch|agent_id_mismatch|misbehavior|error.",
		}, []string{"result"}),
		agentCertPinTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_agent_cert_pin_total",
			Help: "Dial-time SPKI pin verification outcomes per outbound agent TLS handshake. Bounded label enum: ok|mismatch|missing. (S-02)",
		}, []string{"result"}),
		caCertExpiryTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_ca_cert_expiry_timestamp_seconds",
			Help: "Unix timestamp at which the panel's embedded CA certificate expires.",
		}),
		serverCertExpiryTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_server_cert_expiry_timestamp_seconds",
			Help: "Unix timestamp at which the panel's gRPC server certificate expires.",
		}),
		agentCertEarliestExpiryTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_agent_cert_earliest_expiry_timestamp_seconds",
			Help: "Unix timestamp of the earliest cert_expires_at across enrolled agents. 0 when no agents are enrolled or the value has not been sampled yet.",
		}),
	}

	reg.MustRegister(
		mc.httpRequestDuration,
		mc.httpRequestsTotal,
		mc.agentConnected,
		mc.batchQueueDepth,
		mc.batchFlushErrorsTotal,
		mc.batchPersistErrorsTotal,
		mc.batchPersistRetriesTotal,
		mc.batchFlushDuration,
		mc.eventHubDropTotal,
		mc.eventHubSubscribers,
		mc.agentInboundDropsTotal,
		mc.jobQueueDepth,
		mc.lockoutActive,
		mc.jobPersistFailuresTotal,
		mc.unsignedUpdateFallbackTotal,
		mc.retentionPrunedRowsTotal,
		mc.panicRecoveredTotal,
		mc.dbPoolOpen,
		mc.dbPoolInUse,
		mc.dbPoolIdle,
		mc.dbPoolMaxOpen,
		mc.dbPoolWaitTotal,
		mc.dbPoolWaitSeconds,
		mc.dbPoolMaxIdleClosed,
		mc.dbPoolLifetimeClose,
		mc.rateLimitRejectedTotal,
		mc.outboundSupervisorsTotal,
		mc.bootstrapAttemptsTotal,
		mc.agentCertPinTotal,
		mc.caCertExpiryTimestamp,
		mc.serverCertExpiryTimestamp,
		mc.agentCertEarliestExpiryTimestamp,
	)

	// Pre-initialise the per-buffer series to zero so Prometheus rules that
	// reference panvex_batch_queue_depth{buffer="agents"} never see a gap
	// before the first Enqueue call happens.
	for _, buf := range knownBatchBuffers {
		mc.batchQueueDepth.WithLabelValues(buf).Set(0)
		mc.batchFlushErrorsTotal.WithLabelValues(buf, "transient").Add(0)
		mc.batchFlushErrorsTotal.WithLabelValues(buf, "persistent").Add(0)
		mc.batchPersistErrorsTotal.WithLabelValues(buf, "transient").Add(0)
		mc.batchPersistErrorsTotal.WithLabelValues(buf, "persistent").Add(0)
		mc.batchPersistRetriesTotal.WithLabelValues(buf, "success").Add(0)
		mc.batchPersistRetriesTotal.WithLabelValues(buf, "exhausted").Add(0)
	}

	// Pre-initialise the retention counters so dashboards never see a gap
	// for tables that have not yet had a prune opportunity (e.g. a brand
	// new panel with no audit events older than the cutoff).
	for _, table := range retentionPruneTables {
		mc.retentionPrunedRowsTotal.WithLabelValues(table).Add(0)
	}

	// Pre-initialise rate-limit scopes so PromQL alerts on
	// `rate(panvex_ratelimit_rejected_total[1m])` never see an absent
	// series before the first rejection ever happens.
	for _, scope := range rateLimitScopes {
		mc.rateLimitRejectedTotal.WithLabelValues(scope).Add(0)
	}

	// Pre-initialise reverse-mode series so alerts don't see absent metrics
	// on a fresh panel that has not yet run any outbound enrollment.
	mc.outboundSupervisorsTotal.WithLabelValues("outbound").Set(0)
	for _, result := range bootstrapResultLabels {
		mc.bootstrapAttemptsTotal.WithLabelValues(result).Add(0)
	}
	for _, result := range certPinResultLabels {
		mc.agentCertPinTotal.WithLabelValues(result).Add(0)
	}

	return mc
}

// bootstrapResultLabels is the bounded enum of result label values for
// panvex_bootstrap_attempts_total. All values must be pre-initialised so
// PromQL rate() alerts never see an absent series on a fresh panel.
var bootstrapResultLabels = []string{
	"success",
	"expired",
	"mismatch",
	"agent_id_mismatch",
	"misbehavior",
	"error",
}

// certPinResultLabels is the bounded enum of result label values for
// panvex_agent_cert_pin_total. Pre-initialised to zero at startup so
// PromQL rate() alerts never see an absent series on a fresh panel. (S-02)
var certPinResultLabels = []string{"ok", "mismatch", "missing"}

// ObserveBootstrapAttempt increments the bootstrap attempt counter for the
// given result label. Safe to call on a nil receiver (metrics disabled).
func (mc *metricsCollectors) ObserveBootstrapAttempt(result string) {
	if mc == nil {
		return
	}
	mc.bootstrapAttemptsTotal.WithLabelValues(result).Inc()
}

// ObserveAgentCertPin increments the SPKI pin verification counter for the
// given result label ("ok", "mismatch", or "missing"). Called from the
// outbound supervisor's VerifyConnection hook after each TLS handshake.
// Safe to call on a nil receiver (metrics disabled). (S-02)
func (mc *metricsCollectors) ObserveAgentCertPin(result string) {
	if mc == nil {
		return
	}
	mc.agentCertPinTotal.WithLabelValues(result).Inc()
}

// AddOutboundSupervisor increments the outbound supervisor gauge by delta.
// Use +1 when a supervisor is created, -1 when it is removed.
// Safe to call on a nil receiver (metrics disabled).
func (mc *metricsCollectors) AddOutboundSupervisor(delta float64) {
	if mc == nil {
		return
	}
	mc.outboundSupervisorsTotal.WithLabelValues("outbound").Add(delta)
}

// ObserveRateLimitReject increments the per-scope rejection counter.
// Called from withRateLimit when a request is denied.
func (mc *metricsCollectors) ObserveRateLimitReject(scope string) {
	if mc == nil {
		return
	}
	mc.rateLimitRejectedTotal.WithLabelValues(scope).Inc()
}

// observePoolGauges snapshots the instantaneous pool counters onto
// the dbPool* gauges. Cumulative counters (WaitCount, MaxIdleClosed,
// ConnMaxLifetimeClosed) are handled separately by addPoolCounterDeltas
// because the publisher is the only thing that knows the previous
// snapshot needed to compute the delta.
func (mc *metricsCollectors) observePoolGauges(stats sql.DBStats) {
	if mc == nil {
		return
	}
	mc.dbPoolOpen.Set(float64(stats.OpenConnections))
	mc.dbPoolInUse.Set(float64(stats.InUse))
	mc.dbPoolIdle.Set(float64(stats.Idle))
	mc.dbPoolMaxOpen.Set(float64(stats.MaxOpenConnections))
}

// addPoolCounterDeltas turns absolute monotonic counters from sql.DBStats
// into Prometheus Counter increments. Negative deltas (which shouldn't
// happen unless a pool was rebuilt under us) are clamped to zero.
func (mc *metricsCollectors) addPoolCounterDeltas(prev, curr sql.DBStats) {
	if mc == nil {
		return
	}
	if d := curr.WaitCount - prev.WaitCount; d > 0 {
		mc.dbPoolWaitTotal.Add(float64(d))
	}
	if d := curr.WaitDuration - prev.WaitDuration; d > 0 {
		mc.dbPoolWaitSeconds.Add(d.Seconds())
	}
	if d := curr.MaxIdleClosed - prev.MaxIdleClosed; d > 0 {
		mc.dbPoolMaxIdleClosed.Add(float64(d))
	}
	if d := curr.MaxLifetimeClosed - prev.MaxLifetimeClosed; d > 0 {
		mc.dbPoolLifetimeClose.Add(float64(d))
	}
}

// ObserveFlushError satisfies batchMetricsSink. It increments both the legacy
// panvex_batch_flush_errors_total series and the spec-mandated
// panvex_batch_persist_errors_total so operators can migrate dashboards
// without losing history.
func (mc *metricsCollectors) ObserveFlushError(buffer, errorType string) {
	if mc == nil {
		return
	}
	mc.batchFlushErrorsTotal.WithLabelValues(buffer, errorType).Inc()
	mc.batchPersistErrorsTotal.WithLabelValues(buffer, errorType).Inc()
}

// ObserveJobPersistFailure satisfies jobs.MetricsSink (C3).
func (mc *metricsCollectors) ObserveJobPersistFailure() {
	if mc == nil {
		return
	}
	mc.jobPersistFailuresTotal.Inc()
}

// SetQueueDepth satisfies batchMetricsSink.
func (mc *metricsCollectors) SetQueueDepth(buffer string, depth float64) {
	if mc == nil {
		return
	}
	mc.batchQueueDepth.WithLabelValues(buffer).Set(depth)
}

// ObservePersistRetry records the final outcome of a retry sequence for a
// single item — "success" when a retry eventually succeeded, "exhausted" when
// all retry attempts failed and the item was dropped.
func (mc *metricsCollectors) ObservePersistRetry(stream, outcome string) {
	if mc == nil {
		return
	}
	mc.batchPersistRetriesTotal.WithLabelValues(stream, outcome).Inc()
}

// ObserveFlushDuration records the end-to-end latency (in seconds) of a single
// per-stream flush, including any retry attempts. Recorded regardless of
// outcome so operators see real tail latency even when flushes fail.
//
// The histogram is NOT pre-primed with zero-value observations for the known
// streams: a zero-duration sample would skew p50/p95 percentiles that
// dashboards rely on. Series appear lazily on the first real flush per stream.
func (mc *metricsCollectors) ObserveFlushDuration(stream string, seconds float64) {
	if mc == nil {
		return
	}
	mc.batchFlushDuration.WithLabelValues(stream).Observe(seconds)
}

// statusBucket maps an HTTP status code to one of {2xx, 3xx, 4xx, 5xx}.
//
// We use buckets rather than the raw numeric status because:
//   - A handler that returns 404 for every /api/clients/{id} lookup would
//     otherwise create one series per distinct 4xx code; bucketing keeps
//     cardinality tight.
//   - Operators almost always aggregate on the class (e.g. "error rate") and
//     rarely need the exact code — which is still visible in per-endpoint
//     access logs when drilldown is needed.
func statusBucket(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return strconv.Itoa(code)
	}
}

// statusCapture wraps http.ResponseWriter to remember the first status code
// written so the metrics middleware can observe it after the inner handler
// returns. A handler that writes a body without calling WriteHeader implicitly
// returns 200, so we default to 200.
type statusCapture struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (c *statusCapture) WriteHeader(code int) {
	if !c.wroteHeader {
		c.status = code
		c.wroteHeader = true
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *statusCapture) Write(b []byte) (int, error) {
	if !c.wroteHeader {
		c.status = http.StatusOK
		c.wroteHeader = true
	}
	return c.ResponseWriter.Write(b)
}

// Hijack forwards to the underlying ResponseWriter so WebSocket / SSE
// handlers that need to take over the raw connection still work when
// their handler is wrapped by metricsMiddleware. Without this method
// the promotion check `ResponseWriter.(http.Hijacker)` fails and
// coder/websocket responds with 501 Not Implemented, breaking the
// realtime feed. The TCP connection is no longer under the middleware's
// control after a successful hijack; the captured status stays at the
// pre-hijack default (200).
func (c *statusCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := c.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not implement http.Hijacker")
	}
	return hj.Hijack()
}

// Flush forwards to the underlying ResponseWriter to keep streaming
// responses (e.g. SSE) working through the metrics wrapper.
func (c *statusCapture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// metricsMiddleware observes duration and increments the request counter for
// every HTTP response served by the router.
//
// The "path" label is taken from chi.RouteContext(r.Context()).RoutePattern()
// rather than r.URL.Path, so "/api/clients/abc" and "/api/clients/def" both
// fold into "/api/clients/{id}". Routes that did not match any registered
// pattern (NotFound, OPTIONS preflight, static UI) receive the label value
// "unmatched" so a misbehaving client cannot fan out the series.
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.obs == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Skip observing the scrape endpoint itself. Prometheus polls /metrics
		// every 15s (or whatever the operator configures); recording those
		// hits would create a dominant panvex_http_requests_total{path="/metrics"}
		// series that drowns out real traffic and inflates the histogram's
		// total-count. The endpoint already exposes its own health via the
		// standard process_* collectors, so nothing is lost.
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		start := s.now()
		capture := &statusCapture{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(capture, r)

		pattern := chi.RouteContext(r.Context()).RoutePattern()
		if pattern == "" {
			pattern = "unmatched"
		}

		method := r.Method
		bucket := statusBucket(capture.status)
		elapsed := s.now().Sub(start).Seconds()

		s.obs.httpRequestDuration.WithLabelValues(method, pattern, bucket).Observe(elapsed)
		s.obs.httpRequestsTotal.WithLabelValues(method, pattern, bucket).Inc()
	})
}

// handleScrapeMetrics returns a handler that serves the Prometheus text exposition
// format. Auth is a single fixed bearer token from
// PANVEX_METRICS_SCRAPE_TOKEN; no session cookies are consulted so Prometheus
// does not need to log in. When the configured token is empty the caller is
// expected to not register the route at all — see routes().
func (s *Server) handleScrapeMetrics(token string) http.Handler {
	inner := promhttp.HandlerFor(s.obs.registry, promhttp.HandlerOpts{
		Registry: s.obs.registry,
	})
	tokenBytes := []byte(token)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
			w.Header().Set("WWW-Authenticate", `Bearer realm="panvex-metrics"`)
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		presented := []byte(header[len(prefix):])
		// Run ConstantTimeCompare unconditionally so the wall-clock
		// signature does not leak the token's length: pad the presented
		// bytes to the secret length first, run the constant-time
		// comparison on the padded form, then combine with a length
		// check. The length comparison branches on operator-supplied
		// input length vs. configured token length — neither is the
		// secret token's bytes — so it does not introduce a side
		// channel beyond what the attacker already controls. M-13.
		padded := presented
		if len(padded) != len(tokenBytes) {
			padded = make([]byte, len(tokenBytes))
		}
		compared := subtle.ConstantTimeCompare(padded, tokenBytes)
		if compared != 1 || len(presented) != len(tokenBytes) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="panvex-metrics"`)
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// startMetricsPoller runs a loop that refreshes gauges derived from live
// in-memory state (agent connection count, event-hub subscribers, job queue
// depth, lockout count). Counters and histograms are pushed at observation
// time elsewhere and therefore do not need polling.
//
// A 5-second interval keeps the scrape-time value reasonably fresh without
// adding noticeable load; scrape intervals in production are typically 15s+.
func (s *Server) startMetricsPoller(ctx context.Context, interval time.Duration) {
	if s.obs == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}

	s.metricsPollerWG.Add(1)
	go func() {
		defer s.metricsPollerWG.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Refresh once immediately so the first scrape after startup has
		// non-zero values (otherwise a test that scrapes right after New()
		// sees only zeros and cannot tell polling is wired).
		s.refreshPolledMetrics()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.refreshPolledMetrics()
			}
		}
	}()
}

// poolStatsProvider is implemented by storage backends that expose
// their database/sql pool counters. Both postgres.Store and
// sqlite.Store satisfy it; tx-bound stores return zero values.
type poolStatsProvider interface {
	PoolStats() sql.DBStats
}

// refreshPolledMetrics samples in-memory state and updates the corresponding
// Prometheus gauges. Kept intentionally lock-light: reads use the same RLocks
// as the HTTP handlers.
func (s *Server) refreshPolledMetrics() {
	if s.obs == nil {
		return
	}
	// EvaluateAll sweeps every tracked agent at the current time, driving
	// presence transitions (so silent agents flip to offline) and counting
	// only non-offline agents — unlike TrackedCount, which counted stale
	// entries until deregistration (L-8).
	s.obs.agentConnected.Set(float64(s.presence.EvaluateAll(s.now())))
	s.obs.eventHubSubscribers.Set(float64(s.events.SubscriberCount()))
	if s.jobs != nil {
		s.obs.jobQueueDepth.Set(float64(s.jobs.QueueDepth()))
	}
	if s.loginLockout != nil {
		s.obs.lockoutActive.Set(float64(s.loginLockout.ActiveCount(s.now())))
	}
	if s.authority != nil {
		s.obs.caCertExpiryTimestamp.Set(float64(s.authority.certificate.NotAfter.Unix()))
		if na := s.authority.serverCertNotAfter(); !na.IsZero() {
			s.obs.serverCertExpiryTimestamp.Set(float64(na.Unix()))
		}
	}
	s.refreshAgentCertExpiry()
	s.refreshPoolMetrics()
}

// refreshAgentCertExpiry samples the earliest agent certificate expiry.
// Unlike the rest of refreshPolledMetrics this touches the store, so it
// is throttled to once per minute (the poller ticks every 5s). Only
// called from the single poller goroutine — no locking on the
// timestamp field.
func (s *Server) refreshAgentCertExpiry() {
	if s.obs == nil || s.store == nil {
		return
	}
	now := s.now()
	if !s.agentCertExpiryRefreshedAt.IsZero() && now.Sub(s.agentCertExpiryRefreshedAt) < time.Minute {
		return
	}
	s.agentCertExpiryRefreshedAt = now

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		slog.Warn("metrics: list agents for cert expiry failed", "err", err)
		return
	}
	var earliest time.Time
	for _, a := range agents {
		if a.CertExpiresAt == nil {
			continue
		}
		if earliest.IsZero() || a.CertExpiresAt.Before(earliest) {
			earliest = *a.CertExpiresAt
		}
	}
	if earliest.IsZero() {
		s.obs.agentCertEarliestExpiryTimestamp.Set(0)
		return
	}
	s.obs.agentCertEarliestExpiryTimestamp.Set(float64(earliest.Unix()))
}

// refreshPoolMetrics snapshots the storage backend's connection-pool
// stats. Gauges (Open/InUse/Idle/MaxOpen) get Set; cumulative counters
// (Wait/MaxIdleClosed/LifetimeClosed) get Add'd by the delta against
// prevPoolStats so Prometheus sees a monotonically increasing series
// from a fresh per-process zero.
func (s *Server) refreshPoolMetrics() {
	provider, ok := s.store.(poolStatsProvider)
	if !ok {
		return
	}
	curr := provider.PoolStats()
	s.obs.observePoolGauges(curr)
	s.poolStatsMu.Lock()
	prev := s.prevPoolStats
	s.prevPoolStats = curr
	s.poolStatsMu.Unlock()
	s.obs.addPoolCounterDeltas(prev, curr)
}

// metricsShutdown stops the metrics polling goroutine, if any. It is safe to
// call multiple times and when no poller was started (token empty).
func (s *Server) metricsShutdown() {
	if s.metricsPollerCancel != nil {
		s.metricsPollerCancel()
	}
	s.metricsPollerWG.Wait()
}
