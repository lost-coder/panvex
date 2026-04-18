// Package otel sets up OpenTelemetry distributed tracing for the
// Panvex control-plane. It is intentionally minimal (P3-OBS-01 scoped
// delivery): an OTLP/gRPC span exporter, a resource describing the
// service, a parent-based sampler (default), and W3C trace-context +
// baggage propagators.
//
// Metrics are deliberately NOT configured here — Panvex already exposes
// Prometheus /metrics; adding OTLP metrics would be a separate effort.
//
// Enabling:
//
//	export OTEL_EXPORTER_OTLP_ENDPOINT=127.0.0.1:4317
//
// When the env var is empty the control-plane skips Init entirely and
// runs with the no-op TracerProvider (zero overhead).
package otel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds the subset of OTLP exporter knobs the control-plane
// needs. Endpoint is the only required field; all others have sane
// defaults.
type Config struct {
	// Endpoint is the host:port for an OTLP/gRPC collector (e.g.
	// "127.0.0.1:4317" for a local Jaeger/Tempo). Leading scheme
	// (http://, https://) is stripped if present.
	Endpoint string

	// Insecure disables TLS to the collector. Default true, matching
	// the usual local-dev Jaeger/Tempo setup.
	Insecure bool

	// ServiceName is reported as the OTel resource service.name.
	// Defaults to "panvex-control-plane".
	ServiceName string

	// ServiceVersion is reported as service.version. Typically the
	// control-plane's build version string.
	ServiceVersion string

	// ExportTimeout caps a single exporter export attempt. Defaults
	// to 10 seconds, matching the OTel SDK default.
	ExportTimeout time.Duration
}

const (
	defaultServiceName   = "panvex-control-plane"
	defaultExportTimeout = 10 * time.Second
)

// Init configures the global OpenTelemetry TracerProvider and
// propagators. Caller MUST invoke the returned shutdown function
// during graceful shutdown to flush buffered spans; a nil shutdown
// is never returned.
//
// If Endpoint is empty, Init returns a no-op shutdown and nil error
// without touching the global providers, so callers can safely invoke
// it unconditionally.
func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }

	if cfg.Endpoint == "" {
		return noop, nil
	}

	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultServiceName
	}
	if cfg.ExportTimeout <= 0 {
		cfg.ExportTimeout = defaultExportTimeout
	}

	endpoint := stripScheme(cfg.Endpoint)

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithTimeout(cfg.ExportTimeout),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	if err != nil {
		return noop, fmt.Errorf("otlp trace exporter: %w", err)
	}

	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
	}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}
	res, err := resource.New(ctx,
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		// shutdown the exporter we already created so we don't leak a gRPC conn
		_ = exporter.Shutdown(ctx)
		return noop, fmt.Errorf("otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// default sampler is ParentBased(AlwaysSample) — desired.
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown := func(ctx context.Context) error {
		// Order matters: flush+stop the provider first (it owns the
		// batcher that feeds the exporter), then shut down the
		// exporter connection.
		var errs []error
		if err := tp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracer provider shutdown: %w", err))
		}
		if err := exporter.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("exporter shutdown: %w", err))
		}
		return errors.Join(errs...)
	}

	return shutdown, nil
}

// stripScheme removes any http:// or https:// prefix because the OTLP
// gRPC exporter wants bare host:port. We tolerate users pasting the
// full URL from a collector dashboard.
func stripScheme(s string) string {
	for _, prefix := range []string{"http://", "https://", "grpc://"} {
		if len(s) > len(prefix) && s[:len(prefix)] == prefix {
			return s[len(prefix):]
		}
	}
	return s
}
