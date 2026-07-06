// Package metrics owns the Prometheus collector bundle exposed at /metrics by
// the control-plane. The collectors and their HTTP surface (scrape handler +
// timing middleware) live here; the background poller that samples live
// server subsystems stays in the server package because it reads server state.
package metrics

import (
	"database/sql"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Collectors bundles the Prometheus collectors exposed at /metrics for
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
type Collectors struct {
	Registry *prometheus.Registry

	// Now is the clock used by the timing Middleware. Defaults to time.Now
	// in NewCollectors; the server injects its own (possibly fake) clock so
	// middleware timings stay deterministic under test.
	Now func() time.Time

	HTTPRequestDuration *prometheus.HistogramVec
	HTTPRequestsTotal   *prometheus.CounterVec

	AgentConnected prometheus.Gauge

	BatchQueueDepth       *prometheus.GaugeVec
	BatchFlushErrorsTotal *prometheus.CounterVec
	BatchDroppedTotal     *prometheus.CounterVec
	// P2-REL-06: batch_writer retry + persistence error surfacing (H14).
	// persist_errors_total mirrors flush_errors_total with the spec-mandated
	// label names (stream/type instead of buffer/error_type). The older metric
	// is kept in place so dashboards from P2-OBS-01 keep working.
	BatchPersistErrorsTotal  *prometheus.CounterVec
	BatchPersistRetriesTotal *prometheus.CounterVec
	// P2-OBS-03: per-stream flush latency histogram (including retries).
	BatchFlushDuration *prometheus.HistogramVec

	EventHubDropTotal   prometheus.Counter
	EventHubSubscribers prometheus.Gauge

	// D-2: silent-drop visibility for regular-priority inbound agent
	// messages. enqueueInboundAgentMessage uses drop-oldest semantics under
	// backpressure, but the rare "all three non-blocking attempts lost"
	// case (drained slot snatched by a concurrent reader) used to vanish
	// silently. Bumped from the regular-path drop branch only — priority
	// messages still block and never feed this counter. Intentionally
	// label-less to honour the cardinality rule above (no agent_id).
	AgentInboundDropsTotal prometheus.Counter

	JobQueueDepth prometheus.Gauge
	LockoutActive prometheus.Gauge

	// C3: write-behind job persist failures. The jobs service retries on
	// the next mutation, but a wedged DB used to be slog-only; this
	// counter backs the PanvexJobPersistFailures alert.
	JobPersistFailuresTotal prometheus.Counter

	// F3 (audit 2026-06-09): count jobs that enter the failed terminal status.
	JobFailuresTotal prometheus.Counter

	UnsignedUpdateFallbackTotal prometheus.Counter

	// P2-REL-04 / P2-REL-05: per-table row count deleted by the retention
	// worker. Labels are a bounded enum (see retentionPruneTables below) so
	// cardinality stays safe.
	RetentionPrunedRowsTotal *prometheus.CounterVec

	// Q3.U-Q-15: per-goroutine panic-recovery counter so a silently
	// recovered panic surfaces as a Prometheus alert instead of vanishing
	// into a single log line. Labels: goroutine name (bounded enum from
	// the call sites — receive, priority-inbound, audit-effects, etc).
	PanicRecoveredTotal *prometheus.CounterVec

	// Phase-2 §2.1: connection pool visibility. Driven by a periodic
	// publisher goroutine that snapshots store.PoolStats() onto these
	// gauges every 15s. PromQL alert thresholds live in
	// deploy/prometheus/alerts.yaml.
	DBPoolOpen          prometheus.Gauge // currently open connections (in_use + idle)
	DBPoolInUse         prometheus.Gauge // connections actively serving a query
	DBPoolIdle          prometheus.Gauge // idle connections retained in the pool
	DBPoolMaxOpen       prometheus.Gauge // configured upper limit (snapshot)
	DBPoolWaitTotal     prometheus.Counter
	DBPoolWaitSeconds   prometheus.Counter
	DBPoolMaxIdleClosed prometheus.Counter
	DBPoolLifetimeClose prometheus.Counter

	// Phase-2 §2.1: rate-limit rejections by scope. Lets oncall see at
	// a glance whether a flood is hitting login, the agent bootstrap,
	// or the per-user sensitive bucket.
	RateLimitRejectedTotal *prometheus.CounterVec

	// Reverse-mode transport metrics (Task 17).
	// OutboundSupervisorsTotal tracks how many outbound (reverse-mode)
	// supervisors are currently running, labelled by transport mode.
	// Label values: "outbound". Pre-initialised to zero so dashboards see
	// the series even before the first reverse agent is enrolled.
	OutboundSupervisorsTotal *prometheus.GaugeVec
	// BootstrapAttemptsTotal counts EnrollDriver.Run outcomes, labelled by
	// result. Bounded label enum: success|expired|mismatch|agent_id_mismatch|
	// misbehavior|error. Pre-initialised to zero for alert stability.
	BootstrapAttemptsTotal *prometheus.CounterVec
	// AgentCertPinTotal counts dial-time SPKI pin verification outcomes,
	// labelled by result. Bounded enum: ok|mismatch|missing.
	// Pre-initialised to zero for PromQL alert stability. (S-02)
	AgentCertPinTotal *prometheus.CounterVec
	// AgentCertPinPersistFailuresTotal counts issuance-time failures to
	// persist an agent's SPKI pin. A growing value means certs were issued
	// whose pins never reached the store, so the fail-closed outbound dial
	// verifier may reject those agents until the next successful renewal
	// (A1 follow-up).
	AgentCertPinPersistFailuresTotal prometheus.Counter

	// F3 (audit 2026-06-09): certificate expiry surfaced as unix
	// timestamps so PromQL can alert on `x - time() < threshold`.
	// 0 means "not yet sampled / no data" — alert rules must guard
	// with `> 0`.
	CACertExpiryTimestamp            prometheus.Gauge
	ServerCertExpiryTimestamp        prometheus.Gauge
	AgentCertEarliestExpiryTimestamp prometheus.Gauge
}

// rateLimitScopes enumerates every scope label that can appear on
// panvex_ratelimit_rejected_total. Pre-initialised to zero at startup
// (keeps PromQL alerts deterministic).
var rateLimitScopes = []string{"login", "agent_bootstrap", "sensitive", "grpc_connect", "install_script"}

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
	"audit",
	"fallback_state",
}

// NewCollectors constructs and registers all Prometheus collectors
// owned by the control-plane server. Each *Server gets its own registry so
// tests do not fight over the global default registry.
func NewCollectors() *Collectors {
	reg := prometheus.NewRegistry()

	mc := &Collectors{
		Registry: reg,
		Now:      time.Now,
		HTTPRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "panvex_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds, bucketed by method, route pattern, and status bucket.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path", "status"}),
		HTTPRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_http_requests_total",
			Help: "Total number of HTTP requests handled, labelled by method, route pattern, and status bucket.",
		}, []string{"method", "path", "status"}),
		AgentConnected: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_agent_connected",
			Help: "Number of agents currently evaluated as connected (presence state is not offline).",
		}),
		BatchQueueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "panvex_batch_queue_depth",
			Help: "Number of items queued in each batch writer buffer, waiting to be flushed to storage.",
		}, []string{"buffer"}),
		BatchFlushErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_batch_flush_errors_total",
			Help: "Total number of batch flush errors by buffer and error_type (transient|persistent).",
		}, []string{"buffer", "error_type"}),
		BatchDroppedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_batch_dropped_total",
			Help: "Items evicted from a bounded batch-writer buffer under the drop-oldest overflow policy, by buffer. Sustained growth means the store cannot keep up with fleet inflow.",
		}, []string{"buffer"}),
		BatchPersistErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_batch_persist_errors_total",
			Help: "Batch writer persistence errors by stream and type (transient|persistent). A transient increment means an individual retry attempt failed; the persistent counter increments once per item that was ultimately dropped.",
		}, []string{"stream", "type"}),
		BatchPersistRetriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_batch_persist_retries_total",
			Help: "Batch writer retry outcomes by stream and outcome (success|exhausted). Success means a retry eventually succeeded; exhausted means all retries were used up and the item was dropped.",
		}, []string{"stream", "outcome"}),
		BatchFlushDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "panvex_batch_flush_duration_seconds",
			Help: "Per-stream batch flush latency in seconds, measured end-to-end including all retries, regardless of outcome.",
			// Buckets tuned for DB flush latency: sub-millisecond work on a
			// hot local SQLite path up to multi-second tails when retries
			// burn the full backoff schedule on a flaky upstream.
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"stream"}),
		EventHubDropTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_event_hub_drop_total",
			Help: "Total number of events dropped because a subscriber channel was full.",
		}),
		AgentInboundDropsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_agent_inbound_drops_total",
			Help: "Inbound agent messages dropped on the regular-priority queue when the drop-oldest retry races with a concurrent reader. Intentionally label-less to keep cardinality bounded (D-2).",
		}),
		EventHubSubscribers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_event_hub_subscribers",
			Help: "Current number of event-hub subscribers.",
		}),
		JobQueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_job_queue_depth",
			Help: "Current number of jobs in the queued/running state.",
		}),
		LockoutActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_lockout_active",
			Help: "Current number of usernames with an active account lockout.",
		}),
		JobPersistFailuresTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_job_persist_failures_total",
			Help: "Total job write-behind persistence failures (PutJob/PutJobTarget). In-memory state stays ahead of storage until the next successful persist; sustained growth means job state is not durable.",
		}),
		JobFailuresTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_job_failures_total",
			Help: "Jobs that entered the failed terminal status (at least one target failed and none can still succeed).",
		}),
		UnsignedUpdateFallbackTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_unsigned_update_fallback_total",
			Help: "Total number of panel-update applications that fell back to an unsigned manifest.",
		}),
		RetentionPrunedRowsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_retention_pruned_rows_total",
			Help: "Total rows deleted by the retention worker, labelled by table. Bounded enum: audit_events, metric_snapshots, jobs, webhook_outbox, agent_revocations, enrollment_tokens.",
		}, []string{"table"}),
		PanicRecoveredTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_goroutine_panic_recovered_total",
			Help: "Total panics caught by recoverGoroutine, labelled by goroutine name (Q3.U-Q-15).",
		}, []string{"goroutine"}),
		DBPoolOpen: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_db_pool_open_connections",
			Help: "Current number of open database connections (in_use + idle). Sample period: 15s.",
		}),
		DBPoolInUse: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_db_pool_in_use_connections",
			Help: "Number of database connections currently serving a query.",
		}),
		DBPoolIdle: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_db_pool_idle_connections",
			Help: "Number of idle database connections retained in the pool.",
		}),
		DBPoolMaxOpen: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_db_pool_max_open_connections",
			Help: "Configured upper limit on simultaneous open connections (PANVEX_DB_MAX_OPEN_CONNS).",
		}),
		DBPoolWaitTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_db_pool_wait_total",
			Help: "Total number of times a goroutine had to wait for an available connection.",
		}),
		DBPoolWaitSeconds: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_db_pool_wait_seconds_total",
			Help: "Cumulative time goroutines spent waiting for a free connection, in seconds.",
		}),
		DBPoolMaxIdleClosed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_db_pool_max_idle_closed_total",
			Help: "Total connections closed because MaxIdleConns was exceeded.",
		}),
		DBPoolLifetimeClose: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_db_pool_lifetime_closed_total",
			Help: "Total connections closed because ConnMaxLifetime was exceeded.",
		}),
		RateLimitRejectedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_ratelimit_rejected_total",
			Help: "Total rate-limit rejections, labelled by scope. Bounded enum.",
		}, []string{"scope"}),
		OutboundSupervisorsTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "panvex_outbound_supervisors_total",
			Help: "Number of outbound (reverse-mode) supervisors currently active, labelled by mode. Incremented by ensureSupervisor, decremented by removeSupervisor/stopAll.",
		}, []string{"mode"}),
		BootstrapAttemptsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_bootstrap_attempts_total",
			Help: "Reverse-mode bootstrap enrollment attempts by result. Bounded label enum: success|expired|mismatch|agent_id_mismatch|misbehavior|error.",
		}, []string{"result"}),
		AgentCertPinTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "panvex_agent_cert_pin_total",
			Help: "Dial-time SPKI pin verification outcomes per outbound agent TLS handshake. Bounded label enum: ok|mismatch|missing. (S-02)",
		}, []string{"result"}),
		AgentCertPinPersistFailuresTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_agent_cert_pin_persist_failures_total",
			Help: "Issuance-time failures to persist an agent SPKI pin (UpdateAgentCertPin errored). A fail-closed outbound dial may reject affected agents until the next successful renewal.",
		}),
		CACertExpiryTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_ca_cert_expiry_timestamp_seconds",
			Help: "Unix timestamp at which the panel's embedded CA certificate expires.",
		}),
		ServerCertExpiryTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_server_cert_expiry_timestamp_seconds",
			Help: "Unix timestamp at which the panel's gRPC server certificate expires.",
		}),
		AgentCertEarliestExpiryTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_agent_cert_earliest_expiry_timestamp_seconds",
			Help: "Unix timestamp of the earliest cert_expires_at across enrolled agents. 0 when no agents are enrolled or the value has not been sampled yet.",
		}),
	}

	reg.MustRegister(
		mc.HTTPRequestDuration,
		mc.HTTPRequestsTotal,
		mc.AgentConnected,
		mc.BatchQueueDepth,
		mc.BatchFlushErrorsTotal,
		mc.BatchDroppedTotal,
		mc.BatchPersistErrorsTotal,
		mc.BatchPersistRetriesTotal,
		mc.BatchFlushDuration,
		mc.EventHubDropTotal,
		mc.EventHubSubscribers,
		mc.AgentInboundDropsTotal,
		mc.JobQueueDepth,
		mc.LockoutActive,
		mc.JobPersistFailuresTotal,
		mc.JobFailuresTotal,
		mc.UnsignedUpdateFallbackTotal,
		mc.RetentionPrunedRowsTotal,
		mc.PanicRecoveredTotal,
		mc.DBPoolOpen,
		mc.DBPoolInUse,
		mc.DBPoolIdle,
		mc.DBPoolMaxOpen,
		mc.DBPoolWaitTotal,
		mc.DBPoolWaitSeconds,
		mc.DBPoolMaxIdleClosed,
		mc.DBPoolLifetimeClose,
		mc.RateLimitRejectedTotal,
		mc.OutboundSupervisorsTotal,
		mc.BootstrapAttemptsTotal,
		mc.AgentCertPinTotal,
		mc.AgentCertPinPersistFailuresTotal,
		mc.CACertExpiryTimestamp,
		mc.ServerCertExpiryTimestamp,
		mc.AgentCertEarliestExpiryTimestamp,
	)

	// Pre-initialise the per-buffer series to zero so Prometheus rules that
	// reference panvex_batch_queue_depth{buffer="agents"} never see a gap
	// before the first Enqueue call happens.
	for _, buf := range knownBatchBuffers {
		mc.BatchQueueDepth.WithLabelValues(buf).Set(0)
		mc.BatchFlushErrorsTotal.WithLabelValues(buf, "transient").Add(0)
		mc.BatchFlushErrorsTotal.WithLabelValues(buf, "persistent").Add(0)
		mc.BatchDroppedTotal.WithLabelValues(buf).Add(0)
		mc.BatchPersistErrorsTotal.WithLabelValues(buf, "transient").Add(0)
		mc.BatchPersistErrorsTotal.WithLabelValues(buf, "persistent").Add(0)
		mc.BatchPersistRetriesTotal.WithLabelValues(buf, "success").Add(0)
		mc.BatchPersistRetriesTotal.WithLabelValues(buf, "exhausted").Add(0)
	}

	// Pre-initialise the retention counters so dashboards never see a gap
	// for tables that have not yet had a prune opportunity (e.g. a brand
	// new panel with no audit events older than the cutoff).
	for _, table := range retentionPruneTables {
		mc.RetentionPrunedRowsTotal.WithLabelValues(table).Add(0)
	}

	// Pre-initialise rate-limit scopes so PromQL alerts on
	// `rate(panvex_ratelimit_rejected_total[1m])` never see an absent
	// series before the first rejection ever happens.
	for _, scope := range rateLimitScopes {
		mc.RateLimitRejectedTotal.WithLabelValues(scope).Add(0)
	}

	// Pre-initialise reverse-mode series so alerts don't see absent metrics
	// on a fresh panel that has not yet run any outbound enrollment.
	mc.OutboundSupervisorsTotal.WithLabelValues("outbound").Set(0)
	for _, result := range bootstrapResultLabels {
		mc.BootstrapAttemptsTotal.WithLabelValues(result).Add(0)
	}
	for _, result := range certPinResultLabels {
		mc.AgentCertPinTotal.WithLabelValues(result).Add(0)
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
func (c *Collectors) ObserveBootstrapAttempt(result string) {
	if c == nil {
		return
	}
	c.BootstrapAttemptsTotal.WithLabelValues(result).Inc()
}

// ObserveAgentCertPin increments the SPKI pin verification counter for the
// given result label ("ok", "mismatch", or "missing"). Called from the
// outbound supervisor's VerifyConnection hook after each TLS handshake.
// Safe to call on a nil receiver (metrics disabled). (S-02)
func (c *Collectors) ObserveAgentCertPin(result string) {
	if c == nil {
		return
	}
	c.AgentCertPinTotal.WithLabelValues(result).Inc()
}

// ObserveAgentCertPinPersistFailure records a failure to persist a freshly
// issued agent's SPKI pin. Safe to call on a nil receiver (metrics disabled).
func (c *Collectors) ObserveAgentCertPinPersistFailure() {
	if c == nil {
		return
	}
	c.AgentCertPinPersistFailuresTotal.Inc()
}

// AddOutboundSupervisor increments the outbound supervisor gauge by delta.
// Use +1 when a supervisor is created, -1 when it is removed.
// Safe to call on a nil receiver (metrics disabled).
func (c *Collectors) AddOutboundSupervisor(delta float64) {
	if c == nil {
		return
	}
	c.OutboundSupervisorsTotal.WithLabelValues("outbound").Add(delta)
}

// ObserveRateLimitReject increments the per-scope rejection counter.
// Called from withRateLimit when a request is denied.
func (c *Collectors) ObserveRateLimitReject(scope string) {
	if c == nil {
		return
	}
	c.RateLimitRejectedTotal.WithLabelValues(scope).Inc()
}

// ObservePoolGauges snapshots the instantaneous pool counters onto
// the DBPool* gauges. Cumulative counters (WaitCount, MaxIdleClosed,
// ConnMaxLifetimeClosed) are handled separately by AddPoolCounterDeltas
// because the publisher is the only thing that knows the previous
// snapshot needed to compute the delta.
func (c *Collectors) ObservePoolGauges(stats sql.DBStats) {
	if c == nil {
		return
	}
	c.DBPoolOpen.Set(float64(stats.OpenConnections))
	c.DBPoolInUse.Set(float64(stats.InUse))
	c.DBPoolIdle.Set(float64(stats.Idle))
	c.DBPoolMaxOpen.Set(float64(stats.MaxOpenConnections))
}

// AddPoolCounterDeltas turns absolute monotonic counters from sql.DBStats
// into Prometheus Counter increments. Negative deltas (which shouldn't
// happen unless a pool was rebuilt under us) are clamped to zero.
func (c *Collectors) AddPoolCounterDeltas(prev, curr sql.DBStats) {
	if c == nil {
		return
	}
	if d := curr.WaitCount - prev.WaitCount; d > 0 {
		c.DBPoolWaitTotal.Add(float64(d))
	}
	if d := curr.WaitDuration - prev.WaitDuration; d > 0 {
		c.DBPoolWaitSeconds.Add(d.Seconds())
	}
	if d := curr.MaxIdleClosed - prev.MaxIdleClosed; d > 0 {
		c.DBPoolMaxIdleClosed.Add(float64(d))
	}
	if d := curr.MaxLifetimeClosed - prev.MaxLifetimeClosed; d > 0 {
		c.DBPoolLifetimeClose.Add(float64(d))
	}
}

// ObserveFlushError satisfies batchwriter.MetricsSink. It increments both the legacy
// panvex_batch_flush_errors_total series and the spec-mandated
// panvex_batch_persist_errors_total so operators can migrate dashboards
// without losing history.
func (c *Collectors) ObserveFlushError(buffer, errorType string) {
	if c == nil {
		return
	}
	c.BatchFlushErrorsTotal.WithLabelValues(buffer, errorType).Inc()
	c.BatchPersistErrorsTotal.WithLabelValues(buffer, errorType).Inc()
}

// ObserveBufferDrop satisfies batchwriter.MetricsSink (P6-6.1c).
func (c *Collectors) ObserveBufferDrop(buffer string, n int) {
	if c == nil {
		return
	}
	c.BatchDroppedTotal.WithLabelValues(buffer).Add(float64(n))
}

// ObserveJobPersistFailure satisfies jobs.MetricsSink (C3).
func (c *Collectors) ObserveJobPersistFailure() {
	if c == nil {
		return
	}
	c.JobPersistFailuresTotal.Inc()
}

// ObserveJobFailed satisfies jobs.MetricsSink: bumps panvex_job_failures_total
// on every transition into the failed terminal status. Safe on a nil receiver.
func (c *Collectors) ObserveJobFailed() {
	if c == nil {
		return
	}
	c.JobFailuresTotal.Inc()
}

// SetQueueDepth satisfies batchwriter.MetricsSink.
func (c *Collectors) SetQueueDepth(buffer string, depth float64) {
	if c == nil {
		return
	}
	c.BatchQueueDepth.WithLabelValues(buffer).Set(depth)
}

// ObservePersistRetry records the final outcome of a retry sequence for a
// single item — "success" when a retry eventually succeeded, "exhausted" when
// all retry attempts failed and the item was dropped.
func (c *Collectors) ObservePersistRetry(stream, outcome string) {
	if c == nil {
		return
	}
	c.BatchPersistRetriesTotal.WithLabelValues(stream, outcome).Inc()
}

// ObserveFlushDuration records the end-to-end latency (in seconds) of a single
// per-stream flush, including any retry attempts. Recorded regardless of
// outcome so operators see real tail latency even when flushes fail.
//
// The histogram is NOT pre-primed with zero-value observations for the known
// streams: a zero-duration sample would skew p50/p95 percentiles that
// dashboards rely on. Series appear lazily on the first real flush per stream.
func (c *Collectors) ObserveFlushDuration(stream string, seconds float64) {
	if c == nil {
		return
	}
	c.BatchFlushDuration.WithLabelValues(stream).Observe(seconds)
}
