package server

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestRequestIDUnaryInterceptorReusesIncoming(t *testing.T) {
	md := metadata.New(map[string]string{"x-request-id": "incoming-rid"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var seen string
	handler := func(ctx context.Context, _ any) (any, error) {
		seen = requestIDFromContext(ctx)
		return nil, nil
	}
	_, err := requestIDUnaryInterceptor()(ctx, nil, nil, handler)
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if seen != "incoming-rid" {
		t.Fatalf("request id = %q", seen)
	}
}

func TestRequestIDUnaryInterceptorGeneratesWhenMissing(t *testing.T) {
	var seen string
	handler := func(ctx context.Context, _ any) (any, error) {
		seen = requestIDFromContext(ctx)
		return nil, nil
	}
	_, err := requestIDUnaryInterceptor()(context.Background(), nil, nil, handler)
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if seen == "" {
		t.Fatalf("request id was not generated")
	}
}
