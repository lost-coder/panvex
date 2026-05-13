package server

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func extractRequestID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get("x-request-id")
	if len(vals) == 0 {
		return ""
	}
	id := vals[0]
	if !validRequestID(id) {
		return ""
	}
	return id
}

func injectRequestID(ctx context.Context) context.Context {
	id := extractRequestID(ctx)
	if id == "" {
		id = newRequestID()
	}
	ctx = context.WithValue(ctx, requestIDKey{}, id)
	return enrollment.WithRequestID(ctx, id)
}

func requestIDUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(injectRequestID(ctx), req)
	}
}

type wrappedRequestIDStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedRequestIDStream) Context() context.Context { return w.ctx }

func requestIDStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, &wrappedRequestIDStream{ServerStream: ss, ctx: injectRequestID(ss.Context())})
	}
}

// RequestIDUnaryServerInterceptor is the exported entrypoint to the gRPC
// unary interceptor that extracts (or mints) an x-request-id and seeds it
// into both the server-internal request-ID context and the enrollment
// request-ID context — letting cmd/control-plane wire it into the gateway
// gRPC server.
func RequestIDUnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return requestIDUnaryInterceptor()
}

// RequestIDStreamServerInterceptor mirrors RequestIDUnaryServerInterceptor
// for server-streaming RPCs.
func RequestIDStreamServerInterceptor() grpc.StreamServerInterceptor {
	return requestIDStreamInterceptor()
}
