package transport

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type requestIDKey struct{}

// WithRequestID stores rid on ctx so the unary/stream interceptors below
// can attach it to outgoing gRPC metadata as x-request-id.
func WithRequestID(ctx context.Context, rid string) context.Context {
	if rid == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, rid)
}

func requestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// RequestIDUnaryInterceptor is a client-side unary interceptor that copies
// the request_id stored on the call ctx (via WithRequestID) onto outgoing
// gRPC metadata as x-request-id, so the panel can correlate logs across
// the HTTP → gRPC hop.
func RequestIDUnaryInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if rid := requestIDFromContext(ctx); rid != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", rid)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// RequestIDStreamInterceptor mirrors RequestIDUnaryInterceptor for
// streaming RPCs (notably the long-lived Connect bidi stream).
func RequestIDStreamInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if rid := requestIDFromContext(ctx); rid != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", rid)
		}
		return streamer(ctx, desc, cc, method, opts...)
	}
}
