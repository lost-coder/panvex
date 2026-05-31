package logutil

import (
	"log/slog"

	"github.com/lost-coder/panvex/internal/controlplane/server"
)

// NewHandler returns the project's standard slog handler:
//
//	raw text/json → secret redaction → context propagation
//
// Two complementary redaction layers protect secrets in logs:
//
//   - Callsite-level helpers (internal/controlplane/server's logUsername /
//     logIPHash / logSessionID) deliberately hash or elide PII at the point
//     of logging. These remain the first line of defence.
//   - The handler-level redactingHandler installed here is defence-in-depth:
//     it automatically masks the value of any attribute whose key looks
//     sensitive (password, token, secret, session, cookie, authorization,
//     api_key, dsn, …) regardless of caller discipline — including stray
//     attributes from third-party library code. See redact.go.
//
// Redaction runs before the context handler so the request_id it injects is
// never itself masked.
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
	redacting := newRedactingHandler(inner)
	return server.NewSlogContextHandler(redacting)
}
