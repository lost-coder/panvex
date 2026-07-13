package logutil

import (
	"context"
	"log/slog"

	"github.com/lost-coder/panvex/internal/requestid"
)

// slogContextHandler is a slog.Handler that copies the per-request fields
// stored on the context (currently only the request ID) onto every record
// before delegating to the wrapped handler. Any slog call inside an HTTP
// handler — including library code that does not know about request IDs —
// picks up correlation for free.
//
// It lives here (not in controlplane/server) so both the control-plane and
// the agent binary can install request_id propagation without the agent
// transitively importing the server package + its DB layer (R3, audit
// 2026-07-07 §4). The request ID is read via the leaf requestid package,
// which the server's requestIDMiddleware and the agent both write.
type slogContextHandler struct {
	wrapped slog.Handler
}

// NewSlogContextHandler wraps an existing handler so emitted records carry
// request_id when present in ctx. Returns the inner handler unchanged when
// given nil so callers can compose unconditionally.
func NewSlogContextHandler(inner slog.Handler) slog.Handler {
	if inner == nil {
		return nil
	}
	return &slogContextHandler{wrapped: inner}
}

func (h *slogContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.wrapped.Enabled(ctx, level)
}

func (h *slogContextHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := requestid.FromContext(ctx); id != "" {
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
