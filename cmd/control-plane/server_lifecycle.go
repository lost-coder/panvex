package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/server"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

// Server tuning constants. Kept as package-level so tests can assert them
// alongside the server constructors.
const (
	// httpReadHeaderTimeout caps the time spent reading request headers. Guards
	// against Slowloris-style header starvation.
	httpReadHeaderTimeout = 10 * time.Second
	// httpReadTimeout caps the total time to read the request (headers + body).
	// Protects against slow-client DoS that would otherwise stall a goroutine
	// indefinitely on a long body read.
	httpReadTimeout = 30 * time.Second
	// httpWriteTimeout caps the time to write the response. WebSocket
	// connections are hijacked via http.Hijacker before the deadline fires, so
	// SSE/streaming on /api/events is unaffected.
	httpWriteTimeout = 60 * time.Second
	// httpIdleTimeout closes idle keep-alive connections after this period.
	httpIdleTimeout = 120 * time.Second

	// grpcKeepaliveTime tells the server to ping idle clients at this interval
	// so NAT/middlebox connection tracking does not silently drop the TCP
	// session.
	grpcKeepaliveTime = 30 * time.Second
	// grpcKeepaliveTimeout is how long the server waits for a keepalive ack
	// before considering the connection dead.
	grpcKeepaliveTimeout = 10 * time.Second
	// grpcKeepaliveMinTime bounds how aggressively clients are allowed to ping
	// before the server treats it as abusive traffic.
	grpcKeepaliveMinTime = 10 * time.Second
	// grpcMaxMessageSize lifts the default 4 MiB cap so that large discovery
	// snapshots (client lists, runtime inventories) can be exchanged without
	// truncation.
	grpcMaxMessageSize = 16 * 1024 * 1024
)

const (
	// httpShutdownBudget bounds how long httpServer.Shutdown waits for
	// in-flight requests to finish. The previous 5s value was tight
	// enough that long-poll handlers (telemetry stream, agent presence
	// SSE) could be killed mid-write — losing the audit row that the
	// handler hadn't flushed yet. 20s comfortably covers our slowest
	// handler while still leaving room in the K8s grace window for the
	// gRPC drain and the batch_writer flush that follow.
	httpShutdownBudget = 20 * time.Second

	// grpcShutdownBudget caps how long we let GracefulStop drain agent
	// streams. After the budget we call Stop() to force-close any
	// strangler stream so the pod can exit before SIGKILL fires.
	grpcShutdownBudget = 10 * time.Second

	// controlPlaneShutdownGraceMin is the minimum
	// `terminationGracePeriodSeconds` the deployment manifest must set:
	// httpShutdownBudget + grpcShutdownBudget + 10s batch_writer drain
	// + 5s slack for OS-level cleanup. Lower values risk SIGKILL during
	// the audit-event flush, dropping events that already returned 2xx
	// to the client.
	controlPlaneShutdownGraceMin = 45 * time.Second
)

// newControlPlaneHTTPServer builds the control-plane HTTP server with hardened
// timeouts. ReadTimeout caps slow-body DoS; WriteTimeout caps slow-consumer
// stalls. The WebSocket endpoint at /api/events is unaffected because
// coder/websocket hijacks the underlying net.Conn via http.Hijacker before
// WriteTimeout fires on streaming connections.
func newControlPlaneHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
	}
}

// newControlPlaneGRPCServer builds the gRPC server used by the agent gateway,
// with TLS credentials, keepalive pings, an enforcement policy that accepts
// keepalives even when no streams are active, and a 16 MiB message cap so that
// large discovery snapshots do not get truncated.
func newControlPlaneGRPCServer(tlsConfig *tls.Config) *grpc.Server {
	return grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    grpcKeepaliveTime,
			Timeout: grpcKeepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             grpcKeepaliveMinTime,
			PermitWithoutStream: true,
		}),
		grpc.MaxRecvMsgSize(grpcMaxMessageSize),
		grpc.MaxSendMsgSize(grpcMaxMessageSize),
		// P3-OBS-01: propagate W3C trace context from agent calls and
		// create per-RPC server spans. Uses the global TracerProvider,
		// which is the no-op provider unless otelcp.Init installed one.
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
}

// startHTTPServer launches the HTTP server in its own goroutine.
func startHTTPServer(httpServer *http.Server, panelRuntime server.PanelRuntime, httpErrors chan<- error) {
	go func() {
		slog.Info("http server listening", "address", panelRuntime.HTTPListenAddress)
		if panelRuntime.TLSMode == "direct" {
			httpErrors <- httpServer.ListenAndServeTLS(panelRuntime.TLSCertFile, panelRuntime.TLSKeyFile)
			return
		}
		httpErrors <- httpServer.ListenAndServe()
	}()
}

// startGRPCServer launches the gRPC server in its own goroutine.
func startGRPCServer(grpcServer *grpc.Server, grpcListener net.Listener, addr string, httpErrors chan<- error) {
	slog.Info("grpc server listening", "address", addr)
	go func() {
		httpErrors <- grpcServer.Serve(grpcListener)
	}()
}

// shutdownHTTPAndGRPC enforces the documented shutdown ordering: HTTP first
// (stop accepting requests), then gRPC (drain streams). api.Close and
// store.Close run after this via the defer stack in runServe.
//
// Per-step budgets (must sum to less than the deployment's K8s
// terminationGracePeriodSeconds — see controlPlaneShutdownGraceMin):
//
//	HTTP Shutdown            httpShutdownBudget
//	gRPC GracefulStop        grpcShutdownBudget (force-stopped after)
//	batchWriter drain        10s in api.Close, see Server.Close
//
// Each step records its latency so a wedged dependency is visible in logs
// even when the bounded budget hides it from end users.
func shutdownHTTPAndGRPC(httpServer *http.Server, grpcServer *grpc.Server, grpcListener net.Listener) {
	shutdownHTTPServer(httpServer)
	shutdownGRPCServer(grpcServer)
	_ = grpcListener.Close()
}

func shutdownHTTPServer(httpServer *http.Server) {
	httpStart := time.Now()
	httpCtx, cancel := context.WithTimeout(context.Background(), httpShutdownBudget)
	defer cancel()
	if err := httpServer.Shutdown(httpCtx); err != nil {
		slog.Warn("http shutdown error",
			"error", err,
			"latency", time.Since(httpStart),
			"budget", httpShutdownBudget,
		)
		return
	}
	slog.Info("http shutdown complete", "latency", time.Since(httpStart))
}

func shutdownGRPCServer(grpcServer *grpc.Server) {
	grpcStart := time.Now()
	grpcDone := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcDone)
	}()
	// L-18: time.After leaks the underlying timer until it fires; in a
	// shutdown path the early "grpcDone" branch routinely beats the
	// budget by several seconds, so the leak adds up over restarts.
	// Switch to NewTimer + Stop so the timer is reclaimed immediately.
	timer := time.NewTimer(grpcShutdownBudget)
	defer timer.Stop()
	select {
	case <-grpcDone:
		slog.Info("grpc shutdown complete", "latency", time.Since(grpcStart))
	case <-timer.C:
		grpcServer.Stop()
		slog.Warn("grpc shutdown forced after budget",
			"budget", grpcShutdownBudget,
			"latency", time.Since(grpcStart),
		)
	}
}

// waitForServeShutdown blocks until either a restart is requested or one of
// the running servers errors, then triggers shutdown and returns the cause.
func waitForServeShutdown(
	restartRequests <-chan struct{},
	httpErrors <-chan error,
	shutdownServers func(),
) error {
	for {
		select {
		case <-restartRequests:
			shutdownServers()
			return errPanelRestartRequested
		case err := <-httpErrors:
			if errors.Is(err, http.ErrServerClosed) {
				continue
			}
			// Ensure the sibling server (HTTP or gRPC) is also shut down so
			// upstream producers stop before the deferred api.Close runs the
			// final audit batch drain.
			shutdownServers()
			return err
		}
	}
}
