// Package requestid carries a request/correlation ID on a context. It is a
// leaf package (stdlib-only) so that log helpers, the control-plane server,
// the enrollment recorder and the agent can all read/write the same key
// without importing each other — in particular the agent binary must not
// transitively pull the control-plane server or its DB layer just to stamp
// request_id onto logs (R3, audit 2026-07-07 §4).
package requestid

import "context"

type requestIDKey struct{}

// WithRequestID stores rid on ctx. Empty rid is a no-op so callers can
// compose unconditionally.
func WithRequestID(ctx context.Context, rid string) context.Context {
	if rid == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, rid)
}

// FromContext returns the value previously stored by WithRequestID, or "".
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}
