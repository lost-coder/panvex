package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
)

// newMetricsTestServer builds a server with a stable clock and optionally a
// scrape token. It intentionally does not use a storage backend — the metrics
// surface area has no dependency on persistence.
func newMetricsTestServer(t *testing.T, scrapeToken string) *Server {
	t.Helper()
	fixed := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
	srv := New(Options{
		Now:                func() time.Time { return fixed },
		MetricsScrapeToken: scrapeToken,
	})
	t.Cleanup(srv.Close)
	return srv
}

// scrapeMetricsText issues a GET /metrics with the supplied bearer token and
// returns the status code plus the response body. tokenHeader="" means send
// no Authorization header at all.
func scrapeMetricsText(t *testing.T, srv *Server, tokenHeader string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if tokenHeader != "" {
		req.Header.Set("Authorization", "Bearer "+tokenHeader)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	return rec.Code, string(body)
}

func TestNewRegistersMetricsCollectorsWithoutPanic(t *testing.T) {
	// Registration happens inside New() — if any collector name clashes or a
	// label set is malformed, prometheus panics synchronously, so this is
	// sufficient to cover the "registration doesn't panic" requirement.
	srv := newMetricsTestServer(t, "")

	if srv.obs == nil {
		t.Fatal("Server.obs is nil after New()")
	}
	if srv.obs.registry == nil {
		t.Fatal("metrics registry is nil after New()")
	}
}

func TestMetricsMiddlewareObservesHealthzRequest(t *testing.T) {
	srv := newMetricsTestServer(t, "devtoken")

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200", rec.Code)
	}

	// Scrape /metrics and assert the GET /healthz series is present with
	// status="2xx". The exposition format is deterministic so a substring
	// assertion is robust enough.
	status, body := scrapeMetricsText(t, srv, "devtoken")
	if status != http.StatusOK {
		t.Fatalf("/metrics status = %d, want 200", status)
	}
	for _, want := range []string{
		`panvex_http_requests_total{method="GET",path="/healthz",status="2xx"} 1`,
		`panvex_http_request_duration_seconds_count{method="GET",path="/healthz",status="2xx"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing expected metric line %q in:\n%s", want, body)
		}
	}
}

func TestMetricsEndpointRejectsMissingAndWrongToken(t *testing.T) {
	srv := newMetricsTestServer(t, "super-secret")

	cases := []struct {
		name  string
		token string
	}{
		{name: "no header", token: ""},
		{name: "wrong token", token: "nope"},
		{name: "partial match", token: "super-secre"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			status, _ := scrapeMetricsText(t, srv, tc.token)
			if status != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", status)
			}
		})
	}
}

func TestMetricsEndpointAcceptsCorrectToken(t *testing.T) {
	srv := newMetricsTestServer(t, "super-secret")

	// Make one HTTP request so that the HTTP *Vec collectors have at least
	// one child series — Prometheus does not expose HELP/TYPE lines for a
	// *Vec that has never seen a label set.
	warmupReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	srv.Handler().ServeHTTP(httptest.NewRecorder(), warmupReq)

	status, body := scrapeMetricsText(t, srv, "super-secret")
	if status != http.StatusOK {
		t.Fatalf("/metrics status = %d, want 200", status)
	}
	if !strings.Contains(body, "panvex_http_requests_total") {
		t.Fatalf("metrics body missing panvex_http_requests_total:\n%s", body)
	}
}

func TestMetricsEndpointNotRegisteredWhenTokenEmpty(t *testing.T) {
	srv := newMetricsTestServer(t, "")

	// Even a syntactically valid Authorization header must not change the
	// outcome when the route is not registered.
	status, _ := scrapeMetricsText(t, srv, "anything")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when no scrape token configured", status)
	}
}

// TestMetricsPathLabelUsesRoutePattern is the cardinality guard: two requests
// against the same registered route but with different path parameters must
// fold into ONE series. This is what keeps UserID and resource IDs out of the
// Prometheus label space.
func TestMetricsPathLabelUsesRoutePattern(t *testing.T) {
	srv := newMetricsTestServer(t, "t")

	// /api/clients/{id} is behind auth, so both requests 401 — but that is
	// irrelevant to the cardinality property we are asserting: the middleware
	// runs for every response, and chi resolves the route pattern even when
	// subsequent middleware short-circuits with a 401.
	for _, raw := range []string{"/api/clients/abc", "/api/clients/def"} {
		req := httptest.NewRequest(http.MethodGet, raw, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
	}

	_, body := scrapeMetricsText(t, srv, "t")

	// The exact series suffix depends on the final status — we only assert
	// that the path label collapsed to the chi template, NOT the raw IDs.
	if strings.Contains(body, `path="/api/clients/abc"`) || strings.Contains(body, `path="/api/clients/def"`) {
		t.Fatalf("metrics body leaked raw path values (cardinality breach):\n%s", body)
	}
	if !strings.Contains(body, `path="/api/clients/{id}"`) {
		t.Fatalf("expected chi route pattern /api/clients/{id} in metrics, got:\n%s", body)
	}
}

func TestStatusBucket(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{200, "2xx"},
		{204, "2xx"},
		{301, "3xx"},
		{404, "4xx"},
		{429, "4xx"},
		{500, "5xx"},
		{503, "5xx"},
		{100, "100"}, // falls outside 2xx-5xx; raw code kept so oddities are visible
	}

	for _, c := range cases {
		if got := statusBucket(c.code); got != c.want {
			t.Errorf("statusBucket(%d) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestEventHubDropHookIncrementsCounter(t *testing.T) {
	srv := newMetricsTestServer(t, "t")

	// Subscribe once but never drain, then overflow the channel (buf 64) so
	// the drop hook fires. Publish more than the buffer capacity; each
	// overflow call invokes srv.obs.eventHubDropTotal.Inc().
	_, cancel := srv.events.Subscribe()
	defer cancel()

	const overflow = 80
	for i := 0; i < overflow; i++ {
		srv.events.Publish(eventbus.Event{Type: "test.drop"})
	}

	_, body := scrapeMetricsText(t, srv, "t")
	// Channel buffer is 64; at least `overflow-64` events must have been
	// dropped. We assert strict-positive rather than an exact number to avoid
	// coupling the test to the channel capacity constant.
	if !strings.Contains(body, "panvex_event_hub_drop_total") {
		t.Fatalf("panvex_event_hub_drop_total missing from exposition:\n%s", body)
	}
	// Negative regex would be overkill; require that the counter is non-zero.
	for _, bad := range []string{"panvex_event_hub_drop_total 0\n", "panvex_event_hub_drop_total 0.0\n"} {
		if strings.Contains(body, bad) {
			t.Fatalf("expected non-zero drop counter, got %q in:\n%s", bad, body)
		}
	}
}
