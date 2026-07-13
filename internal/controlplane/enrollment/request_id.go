package enrollment

import (
	"context"

	"github.com/lost-coder/panvex/internal/requestid"
)

// WithRequestID and RequestIDFromContext delegate to the leaf requestid
// package so the enrollment Recorder, the control-plane server and the agent
// all share ONE context key (R3). Kept as thin aliases to avoid churning the
// many existing enrollment.WithRequestID / RequestIDFromContext call sites.

// WithRequestID stores rid on ctx so the Recorder can pull it onto attempts.
func WithRequestID(ctx context.Context, rid string) context.Context {
	return requestid.WithRequestID(ctx, rid)
}

// RequestIDFromContext returns the value previously stored by WithRequestID,
// or "" if none.
func RequestIDFromContext(ctx context.Context) string {
	return requestid.FromContext(ctx)
}
