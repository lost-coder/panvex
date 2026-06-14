package transport

import (
	"context"

	"google.golang.org/grpc"
)

// RequestIDUnaryInterceptor is a client-side unary interceptor that would
// attach x-request-id to outgoing gRPC metadata. No callers currently seed
// the call ctx with a request-id (audit 2026-06-09, B7), so this is a no-op
// pass-through retained for the interception chain shape.
func RequestIDUnaryInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// RequestIDStreamInterceptor mirrors RequestIDUnaryInterceptor for
// streaming RPCs (notably the long-lived Connect bidi stream).
func RequestIDStreamInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(ctx, desc, cc, method, opts...)
	}
}
