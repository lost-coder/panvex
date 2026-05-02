package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	otelcp "github.com/lost-coder/panvex/internal/controlplane/otel"
)

// initOtelTracing initialises OpenTelemetry exporters from environment vars.
// P3-OBS-01: when OTEL_EXPORTER_OTLP_ENDPOINT is unset this is a cheap no-op
// so production deployments that do not run a collector pay zero cost.
//
// Insecure transport (no TLS) is opt-in: traces carry request IDs, agent
// IDs, fleet group IDs, paths, and IPs — leaking them in plain HTTP is a
// privacy regression. Set OTEL_EXPORTER_OTLP_INSECURE=true only for
// loopback / sidecar collectors that already terminate TLS upstream.
func initOtelTracing() func(context.Context) error {
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	insecure := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE"))) == "true"
	if endpoint != "" && insecure {
		slog.Warn("OTLP exporter running without TLS; only safe for loopback/sidecar collectors",
			"endpoint", endpoint)
	}
	otelShutdown, err := otelcp.Init(context.Background(), otelcp.Config{
		Endpoint:       endpoint,
		Insecure:       insecure,
		ServiceName:    "panvex-control-plane",
		ServiceVersion: Version,
	})
	if err != nil {
		// Tracing init must never block startup — log and run unsampled.
		slog.Warn("otel init failed; continuing without tracing", "error", err)
	}
	return otelShutdown
}

// shutdownOtel flushes the OTel exporter under a bounded timeout.
func shutdownOtel(otelShutdown func(context.Context) error) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := otelShutdown(shutdownCtx); err != nil {
		slog.Warn("otel shutdown error", "error", err)
	}
}
