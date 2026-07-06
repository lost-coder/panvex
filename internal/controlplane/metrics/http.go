package metrics

import (
	"bufio"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

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
// their handler is wrapped by the metrics Middleware. Without this method
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

// Middleware observes duration and increments the request counter for
// every HTTP response served by the router.
//
// The "path" label is taken from chi.RouteContext(r.Context()).RoutePattern()
// rather than r.URL.Path, so "/api/clients/abc" and "/api/clients/def" both
// fold into "/api/clients/{id}". Routes that did not match any registered
// pattern (NotFound, OPTIONS preflight, static UI) receive the label value
// "unmatched" so a misbehaving client cannot fan out the series.
func (c *Collectors) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c == nil {
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

		now := c.Now
		if now == nil {
			now = time.Now
		}

		start := now()
		capture := &statusCapture{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(capture, r)

		pattern := chi.RouteContext(r.Context()).RoutePattern()
		if pattern == "" {
			pattern = "unmatched"
		}

		method := r.Method
		bucket := statusBucket(capture.status)
		elapsed := now().Sub(start).Seconds()

		c.HTTPRequestDuration.WithLabelValues(method, pattern, bucket).Observe(elapsed)
		c.HTTPRequestsTotal.WithLabelValues(method, pattern, bucket).Inc()
	})
}

// ScrapeHandler returns a handler that serves the Prometheus text exposition
// format. Auth is a single fixed bearer token from
// PANVEX_METRICS_SCRAPE_TOKEN; no session cookies are consulted so Prometheus
// does not need to log in. When the configured token is empty the caller is
// expected to not register the route at all — see routes().
func (c *Collectors) ScrapeHandler(token string) http.Handler {
	inner := promhttp.HandlerFor(c.Registry, promhttp.HandlerOpts{
		Registry: c.Registry,
	})
	tokenBytes := []byte(token)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
			w.Header().Set("WWW-Authenticate", `Bearer realm="panvex-metrics"`)
			writeMetricsAuthError(w, "missing bearer token")
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
			writeMetricsAuthError(w, "invalid bearer token")
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// writeMetricsAuthError writes a 401 JSON body of the shape {"error": msg}.
// It mirrors the server package's writeError contract — including the
// scrubErrorMessage pass — without importing it (server imports metrics, so a
// back-import would cycle). The scrub is behaviour-preserving: the two auth
// messages both contain "token" and therefore collapse to "internal error",
// exactly as the pre-extraction handler returned.
func writeMetricsAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": scrubErrorMessage(msg)})
}

// scrubErrorMessage is a verbatim copy of the server package's helper: any
// message mentioning a sensitive keyword collapses to a generic string so an
// error body cannot leak secrets. Duplicated (not imported) to keep metrics
// free of a server dependency.
func scrubErrorMessage(message string) string {
	lower := strings.ToLower(message)
	for _, needle := range []string{"password", "secret", "token", "ciphertext", "private key", "passphrase"} {
		if strings.Contains(lower, needle) {
			return "internal error"
		}
	}
	return message
}
