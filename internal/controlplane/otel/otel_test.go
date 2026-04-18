package otel

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestInit_NoEndpoint verifies the control-plane can call Init
// unconditionally at startup: when OTEL_EXPORTER_OTLP_ENDPOINT is
// unset (Config.Endpoint==""), Init must be a cheap no-op — no
// provider mutation, no background goroutines, no error.
func TestInit_NoEndpoint(t *testing.T) {
	t.Parallel()

	before := otel.GetTracerProvider()

	shutdown, err := Init(context.Background(), Config{})
	if err != nil {
		t.Fatalf("Init with empty endpoint returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init returned nil shutdown")
	}

	if otel.GetTracerProvider() != before {
		t.Fatal("Init with empty endpoint mutated the global TracerProvider")
	}

	// shutdown must also be a harmless no-op.
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown returned error: %v", err)
	}
}

// TestInit_WithEndpoint checks that supplying an Endpoint installs a
// real SDK TracerProvider and that shutdown tears it down without
// blocking startup, even when the collector is unreachable. We do not
// require successful export — that would need a running collector and
// belongs in an integration test.
func TestInit_WithEndpoint(t *testing.T) {
	t.Parallel()

	// Use a port that's almost certainly closed. The exporter is lazy —
	// it doesn't dial on New, only on export — so construction succeeds.
	cfg := Config{
		Endpoint:       "127.0.0.1:1",
		Insecure:       true,
		ServiceName:    "panvex-control-plane-test",
		ServiceVersion: "0.0.0-test",
		ExportTimeout:  100 * time.Millisecond,
	}

	shutdown, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	t.Cleanup(func() {
		// Restore the default no-op provider so other tests aren't affected.
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
	})

	tp := otel.GetTracerProvider()
	if _, ok := tp.(*sdktrace.TracerProvider); !ok {
		t.Fatalf("expected *sdktrace.TracerProvider, got %T", tp)
	}

	// Shutdown must respect the caller's context deadline and not hang
	// even when the collector can never be reached.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	// Error is acceptable (unreachable endpoint) — we only assert the
	// call returns within the deadline.
	_ = shutdown(ctx)
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatal("shutdown exceeded timeout")
	}
}

func TestStripScheme(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"localhost:4317":          "localhost:4317",
		"http://localhost:4317":   "localhost:4317",
		"https://otel.example:1": "otel.example:1",
		"grpc://tempo:4317":       "tempo:4317",
	}
	for in, want := range cases {
		if got := stripScheme(in); got != want {
			t.Errorf("stripScheme(%q) = %q, want %q", in, got, want)
		}
	}
}
