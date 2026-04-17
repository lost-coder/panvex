package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// handleHealthz returns 200 OK when the HTTP server is accepting connections (liveness probe).
func handleHealthz() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// handleReadyz returns 200 OK when the server is ready to serve traffic (readiness probe).
// The response body is intentionally generic — infrastructure failure details are logged
// rather than exposed to unauthenticated callers to avoid revealing internal state
// (certificate authority status, storage backend liveness) to the network.
func (s *Server) handleReadyz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if s.authority == nil {
			slog.Warn("readyz: certificate authority not initialized")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
			return
		}
		if s.store != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := s.store.Ping(ctx); err != nil {
				slog.Warn("readyz: storage unreachable", "err", err)
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("not ready"))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	}
}
