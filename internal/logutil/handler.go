package logutil

import (
	"log/slog"

	"github.com/lost-coder/panvex/internal/controlplane/server"
)

// NewHandler returns the project's standard slog handler:
//
//	raw text/json → context propagation
//
// Redaction of sensitive fields (usernames, session IDs, IP addresses)
// is callsite-level in this project — see internal/controlplane/server's
// logUsername / logIPHash / logSessionID helpers. There is no
// handler-level redaction wrapper.
//
// All cmd entry points (cmd/control-plane, cmd/agent) MUST use this
// constructor so the request_id context propagation always applies.
func NewHandler(opts Options) slog.Handler {
	sink := opts.resolveSink()
	handlerOpts := &slog.HandlerOptions{Level: opts.Level}

	var inner slog.Handler
	switch opts.Format {
	case FormatJSON:
		inner = slog.NewJSONHandler(sink, handlerOpts)
	default:
		inner = slog.NewTextHandler(sink, handlerOpts)
	}
	return server.NewSlogContextHandler(inner)
}
