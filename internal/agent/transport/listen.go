package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

// Symmetric keepalive + message caps (A1): mirror the panel's inbound gRPC
// server (cmd/control-plane/server_lifecycle.go) so reverse streams survive
// NAT idle timeouts and >4 MiB snapshots.
const (
	listenKeepaliveTime    = 30 * time.Second
	listenKeepaliveTimeout = 10 * time.Second
	listenKeepaliveMinTime = 10 * time.Second
	listenMaxMessageSize   = 16 * 1024 * 1024
)

// ListenConfig holds the inputs for the agent's mTLS gRPC server in
// reverse mode (panel dials the agent).
type ListenConfig struct {
	Addr  string          // e.g. ":8443" or "0.0.0.0:8443"
	Cert  tls.Certificate // agent's leaf cert (signed by panel CA after enrollment)
	CAPEM string          // panel CA PEM — agent verifies the panel's client cert against this
	// PanelCN is the CommonName the dialing panel's client certificate must
	// carry (protocol constant "control-plane.panvex.internal"). Verified on
	// top of RequireAndVerifyClientCert chain validation so a leaked AGENT
	// certificate (same CA, CN=<agent_id>) cannot drive this agent's job
	// pipeline (A1). Required: buildServerTLS fails on empty value.
	PanelCN string
}

type listenTransport struct {
	cfg ListenConfig

	mu       sync.Mutex
	listener net.Listener
}

// Compile-time check: *listenTransport must satisfy Transport.
var _ Transport = (*listenTransport)(nil)

// NewListenTransport returns a Transport that accepts a panel-initiated
// Connect stream and routes it into the SessionRunner. Each call to RunOnce
// accepts at most one stream and returns when the stream ends.
func NewListenTransport(cfg ListenConfig) Transport { return &listenTransport{cfg: cfg} }

// Address returns the bound listener address (useful in tests where Addr
// is "127.0.0.1:0"). Returns "" if RunOnce hasn't started yet.
func (t *listenTransport) Address() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.listener == nil {
		return ""
	}
	return t.listener.Addr().String()
}

// RunOnce starts an mTLS gRPC server, waits for the panel to dial in and
// open a Connect stream, dispatches the stream to runner, and returns when
// the stream ends or ctx is cancelled.
//
// The client argument passed to runner is always nil in listen mode because
// the agent does not hold an outbound AgentGatewayClient connection.
// Callers must not invoke cert-renewal-over-stream from within the runner in
// this mode.
func (t *listenTransport) RunOnce(ctx context.Context, runner SessionRunner) error {
	tlsCfg, err := buildServerTLS(t.cfg)
	if err != nil {
		return fmt.Errorf("listen: build tls: %w", err)
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", t.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: bind: %w", err)
	}
	t.mu.Lock()
	t.listener = listener
	t.mu.Unlock()

	server := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsCfg)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    listenKeepaliveTime,
			Timeout: listenKeepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             listenKeepaliveMinTime,
			PermitWithoutStream: true,
		}),
		grpc.MaxRecvMsgSize(listenMaxMessageSize),
		grpc.MaxSendMsgSize(listenMaxMessageSize),
	)

	// Per-stream completion gate — once one Connect arrives and ends, RunOnce
	// returns (mirroring dial-mode's "one connection per RunOnce" semantics).
	done := make(chan error, 1)
	gatewayrpc.RegisterAgentGatewayServer(server, &listenAgentServer{
		runner: runner,
		done:   done,
	})

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	// ctx-bound graceful stop scoped to this RunOnce invocation. Without the
	// local stopCtx, the goroutine would leak until the outer ctx is cancelled
	// — one leaked goroutine per reconnect cycle in long-running agents (B-1).
	stopCtx, stopCancel := context.WithCancel(ctx)
	defer stopCancel()
	go func() {
		<-stopCtx.Done()
		server.GracefulStop()
	}()

	// Wait for either:
	//  - the runner to finish (one stream completed)
	//  - the caller to cancel (handled by graceful-stop goroutine above)
	//  - Serve to fail/return unexpectedly
	var runErr error
	select {
	case runErr = <-done:
	case <-ctx.Done():
		runErr = ctx.Err()
	case runErr = <-serveErr:
		return runErr
	}
	server.GracefulStop()
	if sErr := <-serveErr; sErr != nil && !errors.Is(sErr, grpc.ErrServerStopped) {
		// Serve error after a successful runner is unusual; surface only
		// if runErr didn't already capture a real failure.
		if runErr == nil {
			runErr = fmt.Errorf("listen: serve: %w", sErr)
		}
	}
	return runErr
}

func buildServerTLS(cfg ListenConfig) (*tls.Config, error) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(cfg.CAPEM)) {
		return nil, fmt.Errorf("listen: invalid CA PEM")
	}
	if cfg.PanelCN == "" {
		return nil, fmt.Errorf("listen: PanelCN is required")
	}
	panelCN := cfg.PanelCN
	return &tls.Config{
		Certificates: []tls.Certificate{cfg.Cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    roots,
		MinVersion:   tls.VersionTLS13,
		// VerifyConnection (not VerifyPeerCertificate) so the panel-CN check
		// also runs on RESUMED TLS sessions — VerifyPeerCertificate is skipped
		// on resumption, which would let a resumed session bypass the CN gate
		// (gosec G123). Under RequireAndVerifyClientCert the standard chain
		// verification has already populated VerifiedChains; we check the
		// VERIFIED leaf, not the raw presented cert.
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
				return fmt.Errorf("listen: no verified client chain")
			}
			leaf := state.VerifiedChains[0][0]
			if leaf.Subject.CommonName != panelCN {
				return fmt.Errorf("listen: client cert CN %q is not the panel client CN %q",
					leaf.Subject.CommonName, panelCN)
			}
			return nil
		},
	}, nil
}

// listenAgentServer is the gRPC server-side handler. Each Connect call
// represents one stream from the panel; we dispatch into the SessionRunner
// and send the result on `done` so RunOnce can return.
type listenAgentServer struct {
	gatewayrpc.UnimplementedAgentGatewayServer
	runner SessionRunner
	done   chan<- error
}

func (s *listenAgentServer) Connect(stream gatewayrpc.AgentGateway_ConnectServer) error {
	// Wrap the gRPC server-side stream as an agent-perspective BidiStream.
	// The proto types are inverted in reverse mode: the agent (gRPC server)
	// SENDS ConnectClientMessage and RECEIVES ConnectServerMessage. Use
	// untyped SendMsg/RecvMsg to transmit the inverted message types — the
	// wire bytes are protobuf-identical regardless of the stream's native typing.
	adapter := &serverStreamAdapter{stream: stream}
	err := s.runner(stream.Context(), adapter, nil /* listen mode: no AgentGatewayClient */)
	// Surface the runner's exit to RunOnce. Non-blocking send so multiple
	// Connect calls (shouldn't happen in our one-shot RunOnce model) don't
	// block on a full channel.
	select {
	case s.done <- err:
	default:
	}
	return err
}

// serverStreamAdapter wraps the typed AgentGateway_ConnectServer to satisfy
// BidiStream with the inverted message-type semantics for reverse mode.
//
// In the proto definition, Connect is typed from the gRPC-client's perspective:
// the gRPC client (panel) sends ConnectClientMessage and the gRPC server (agent)
// sends ConnectServerMessage. In reverse mode the application-layer roles are
// swapped: the agent sends ConnectClientMessage and receives ConnectServerMessage.
// We use untyped SendMsg/RecvMsg to bypass the stream's native type constraints.
type serverStreamAdapter struct {
	stream gatewayrpc.AgentGateway_ConnectServer
}

func (a *serverStreamAdapter) Send(msg *gatewayrpc.ConnectClientMessage) error {
	return a.stream.SendMsg(msg)
}

func (a *serverStreamAdapter) Recv() (*gatewayrpc.ConnectServerMessage, error) {
	msg := &gatewayrpc.ConnectServerMessage{}
	if err := a.stream.RecvMsg(msg); err != nil {
		return nil, err
	}
	return msg, nil
}
