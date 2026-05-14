package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
)

// requestIDHeader is the canonical header used both inbound (clients
// can pass their own correlation ID for end-to-end tracing) and
// outbound (every response advertises its server-side ID).
const requestIDHeader = "X-Request-Id"

type requestIDKey struct{}

// requestIDFromContext returns the request ID stored on ctx, or "" if
// the request was not routed through requestIDMiddleware.
func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(requestIDKey{}).(string)
	return v
}

// requestIDMiddleware ensures every request has a stable correlation
// ID exposed both on the response (so the client can quote it in a bug
// report) and on the request context (so every slog line emitted by
// downstream handlers can include the ID via slogContextHandler below).
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
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
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

// slogContextHandler is a slog.Handler that copies the per-request
// fields stored on the context (currently only the request ID) onto
// every record before delegating to the wrapped handler. This means
// any slog call inside an HTTP handler — including library code that
// does not know about request IDs — picks up correlation for free.
type slogContextHandler struct {
	wrapped slog.Handler
}

// NewSlogContextHandler wraps an existing handler so emitted records
// carry request_id when present in ctx. Returns the inner handler
// unchanged when given nil so callers can compose unconditionally.
// Exported for cmd/control-plane to install at startup.
func NewSlogContextHandler(inner slog.Handler) slog.Handler {
	return newSlogContextHandler(inner)
}

func newSlogContextHandler(inner slog.Handler) slog.Handler {
	if inner == nil {
		return nil
	}
	return &slogContextHandler{wrapped: inner}
}

func (h *slogContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.wrapped.Enabled(ctx, level)
}

func (h *slogContextHandler) Handle(ctx context.Context, record slog.Record) error {
	id := requestIDFromContext(ctx)
	if id == "" {
		// Fallback so callers that propagate request_id via the
		// enrollment package's helper (e.g. cmd/agent, integration
		// tests in non-server packages) still benefit from the
		// structured attribute.
		id = enrollment.RequestIDFromContext(ctx)
	}
	if id != "" {
		record.AddAttrs(slog.String("request_id", id))
	}
	return h.wrapped.Handle(ctx, record)
}

func (h *slogContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &slogContextHandler{wrapped: h.wrapped.WithAttrs(attrs)}
}

func (h *slogContextHandler) WithGroup(name string) slog.Handler {
	return &slogContextHandler{wrapped: h.wrapped.WithGroup(name)}
}
