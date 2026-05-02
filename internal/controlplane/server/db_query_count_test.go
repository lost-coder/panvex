package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// TestDBQueryCountMiddlewareUnderThresholdNoWarn asserts the middleware
// stays silent when query counts are normal — important so dashboards
// aren't drowned in noise. (P-02)
func TestDBQueryCountMiddlewareUnderThresholdNoWarn(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv := &Server{logger: logger}

	handler := srv.dbQueryCountMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Simulate 10 queries — well under the threshold.
		for i := 0; i < 10; i++ {
			storage.IncrementDBQuery(r.Context())
		}
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	handler.ServeHTTP(rr, req)

	if strings.Contains(buf.String(), "high_db_query_count") {
		t.Fatalf("WARN fired below threshold:\n%s", buf.String())
	}
}

// TestDBQueryCountMiddlewareAboveThresholdWarns asserts the middleware
// emits a structured WARN with the audit-stable alert key when the
// per-request query count exceeds highQueryCountThreshold.
func TestDBQueryCountMiddlewareAboveThresholdWarns(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv := &Server{logger: logger}

	handler := srv.dbQueryCountMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Simulate threshold + 5 queries to cross the line.
		for i := 0; i < highQueryCountThreshold+5; i++ {
			storage.IncrementDBQuery(r.Context())
		}
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	handler.ServeHTTP(rr, req)

	out := buf.String()
	if !strings.Contains(out, "alert=high_db_query_count") {
		t.Fatalf("WARN missing audit alert key:\n%s", out)
	}
	if !strings.Contains(out, "/api/dashboard") {
		t.Fatalf("WARN missing request path:\n%s", out)
	}
	if !strings.Contains(out, "query_count=") {
		t.Fatalf("WARN missing query_count attr:\n%s", out)
	}
}

// TestDBQueryCountMiddlewareNoLoggerNoCrash asserts the middleware is safe
// when no logger is attached (test environments, edge cases).
func TestDBQueryCountMiddlewareNoLoggerNoCrash(t *testing.T) {
	t.Parallel()
	srv := &Server{logger: nil}
	handler := srv.dbQueryCountMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		for i := 0; i < highQueryCountThreshold+5; i++ {
			storage.IncrementDBQuery(r.Context())
		}
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	handler.ServeHTTP(rr, req) // must not panic
}
