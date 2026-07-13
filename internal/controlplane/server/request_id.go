package server

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/requestid"
)

// requestIDHeader is the canonical header used both inbound (clients
// can pass their own correlation ID for end-to-end tracing) and
// outbound (every response advertises its server-side ID).
const requestIDHeader = "X-Request-Id"

// requestIDMiddleware ensures every request has a stable correlation
// ID exposed both on the response (so the client can quote it in a bug
// report) and on the request context (so every slog line emitted by
// downstream handlers can include the ID via logutil.NewSlogContextHandler).
//
// If the client supplies an X-Request-Id we trust it after a basic
// sanity check (printable ASCII, ≤128 bytes) — that lets a reverse
// proxy correlate panel→backend logs end-to-end. Otherwise we mint a
// UUID v7: time-ordered so logs sort naturally even when ingest
// reorders events.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		// requestid is the leaf carrier read by logutil's context handler;
		// enrollment.WithRequestID delegates to the same key, so the
		// enrollment Recorder sees the id too (R3).
		ctx := requestid.WithRequestID(r.Context(), id)
		ctx = enrollment.WithRequestID(ctx, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	v, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 only fails when crypto/rand fails — extremely rare.
		// Fall back to V4 so we never hand out an empty ID.
		return uuid.NewString()
	}
	return v.String()
}

// validRequestID accepts a small subset of printable ASCII. Anything
// with whitespace, control bytes, or extreme length is replaced with a
// fresh UUID — preserving the operator-supplied correlation when it is
// well-formed and refusing to leak attacker-controlled bytes into logs
// otherwise.
func validRequestID(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}
