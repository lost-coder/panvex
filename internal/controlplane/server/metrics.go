package server

import (
	"context"
	"crypto/subtle"
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

	eventHubDropTotal   prometheus.Counter
	eventHubSubscribers prometheus.Gauge

	jobQueueDepth prometheus.Gauge
	lockoutActive prometheus.Gauge

	auditBufferDepth prometheus.Gauge

	unsignedUpdateFallbackTotal prometheus.Counter
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
			Help: "Number of agents currently tracked as connected (has a non-empty presence entry).",
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
		eventHubDropTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_event_hub_drop_total",
			Help: "Total number of events dropped because a subscriber channel was full.",
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
		auditBufferDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "panvex_audit_buffer_depth",
			Help: "Current depth of the audit-event buffer (pre-registered for P2-LOG-10).",
		}),
		unsignedUpdateFallbackTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "panvex_unsigned_update_fallback_total",
			Help: "Total number of panel-update applications that fell back to an unsigned manifest.",
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
		mc.eventHubDropTotal,
		mc.eventHubSubscribers,
		mc.jobQueueDepth,
		mc.lockoutActive,
		mc.auditBufferDepth,
		mc.unsignedUpdateFallbackTotal,
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

	return mc
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
		// Equal-length tokens are compared in constant time so callers cannot
		// time-oracle the token bytes. The token length itself is not hidden —
		// a mismatched-length check short-circuits before ConstantTimeCompare.
		// Acceptable here because the operator chooses the token and it has no
		// secret length (treat it as a fixed >=32-byte random string).
		if len(presented) != len(tokenBytes) || subtle.ConstantTimeCompare(presented, tokenBytes) != 1 {
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

// refreshPolledMetrics samples in-memory state and updates the corresponding
// Prometheus gauges. Kept intentionally lock-light: reads use the same RLocks
// as the HTTP handlers.
func (s *Server) refreshPolledMetrics() {
	if s.obs == nil {
		return
	}
	s.obs.agentConnected.Set(float64(s.presence.TrackedCount()))
	s.obs.eventHubSubscribers.Set(float64(s.events.subscriberCount()))
	if s.jobs != nil {
		s.obs.jobQueueDepth.Set(float64(s.jobs.QueueDepth()))
	}
	if s.loginLockout != nil {
		s.obs.lockoutActive.Set(float64(s.loginLockout.ActiveCount(s.now())))
	}
	s.obs.auditBufferDepth.Set(float64(s.auditBufferLen()))
}

// auditBufferLen returns the current length of the in-memory audit ring. It
// is used by the metrics poller to expose panvex_audit_buffer_depth.
func (s *Server) auditBufferLen() int {
	s.metricsAuditMu.RLock()
	defer s.metricsAuditMu.RUnlock()
	return len(s.auditTrail)
}

// metricsShutdown stops the metrics polling goroutine, if any. It is safe to
// call multiple times and when no poller was started (token empty).
func (s *Server) metricsShutdown() {
	if s.metricsPollerCancel != nil {
		s.metricsPollerCancel()
	}
	s.metricsPollerWG.Wait()
}

