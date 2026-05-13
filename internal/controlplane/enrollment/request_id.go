package enrollment

import "context"

type requestIDKey struct{}

// WithRequestID stores rid on ctx so the Recorder can pull it onto attempts
// without depending on the controlplane/server package.
func WithRequestID(ctx context.Context, rid string) context.Context {
	if rid == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, rid)
}

// RequestIDFromContext returns the value previously stored by WithRequestID,
// or "" if none.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}
