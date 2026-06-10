package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestInstallAgentScriptIsRateLimited verifies the per-IP rate limit on the
// public /install-agent.sh route (Task 13 — A5 follow-up). The first N
// requests from a given IP must succeed; the (N+1)-th must receive 429. A
// second IP must have its own independent bucket.
func TestInstallAgentScriptIsRateLimited(t *testing.T) {
	now := time.Date(2026, time.June, 10, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	handler := server.Handler()

	for i := 0; i < httpInstallScriptRateLimitPerWindow; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/install-agent.sh", nil)
		req.RemoteAddr = "203.0.113.7:1234"
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d/%d status = %d, want 200", i+1, httpInstallScriptRateLimitPerWindow, rec.Code)
		}
	}

	// N+1-th request from the same IP must be rate-limited.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/install-agent.sh", nil)
	req.RemoteAddr = "203.0.113.7:1234"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-limit status = %d, want 429", rec.Code)
	}

	// A different IP must have its own bucket — first request must succeed.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/install-agent.sh", nil)
	req.RemoteAddr = "203.0.113.8:1234"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fresh-ip status = %d, want 200 (must not share bucket with first IP)", rec.Code)
	}
}
