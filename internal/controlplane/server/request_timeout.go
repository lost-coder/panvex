package server

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// defaultRequestTimeout bounds how long a non-streaming HTTP handler
// may spend before the server aborts it. It sits comfortably below
// httpWriteTimeout (60s, enforced on the connection by net/http) so
// the request context cancels the handler first, giving it a clean
// exit path to emit a 503 via the caller's own error reporting rather
// than a half-written response on a torn-down socket.
//
// 45s is deliberately slack: typical endpoints finish in well under a
// second, so raising the ceiling above worst-case DB/telemetry fan-out
// work matters more than tightening it further for average case.
const defaultRequestTimeout = 45 * time.Second

// streamingPathSuffixes enumerates the tail of routes that MUST NOT
// have a per-request timeout applied. Long-lived WebSocket / SSE
// streams hijack the underlying connection and manage their own
// lifecycle via the parent ctx cancellation; imposing a 45s deadline
// would tear them down prematurely.
var streamingPathSuffixes = []string{
	"/events",
}

// requestTimeoutMiddleware (B8) applies a per-request deadline to the
// handler's context so a wedged dependency (slow DB query, jammed
// external call) does not tie up a goroutine and writer slot forever.
// Paths matching streamingPathSuffixes are exempt — they supply their
// own lifetime semantics.
func requestTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isStreamingPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func isStreamingPath(p string) bool {
	for _, suffix := range streamingPathSuffixes {
		if strings.HasSuffix(p, suffix) {
			return true
		}
	}
	return false
}
