package server

import "net/http"

// handleHealthz returns 200 OK when the HTTP server is accepting connections (liveness probe).
func handleHealthz() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}

// handleReadyz returns 200 OK when the server is ready to serve traffic (readiness probe).
// It verifies that the certificate authority has been initialized.
func (s *Server) handleReadyz() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if s.authority == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("not ready: certificate authority not initialized"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}
