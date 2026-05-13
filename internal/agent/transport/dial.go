package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

const (
	dialKeepaliveTime    = 30 * time.Second
	dialKeepaliveTimeout = 10 * time.Second
	dialMaxMessageSize   = 16 * 1024 * 1024
)

// DialConfig holds the inputs needed to dial the panel's AgentGateway over mTLS.
type DialConfig struct {
	GatewayAddr    string
	ServerName     string
	CAPEM          string
	Cert           tls.Certificate
	// ConnectTimeout is the maximum time to wait for client.Connect to return
	// a stream. Zero means no timeout.
	ConnectTimeout time.Duration
}

type dialTransport struct {
	cfg DialConfig
}

// NewDialTransport returns a Transport that dials the panel using mTLS.
func NewDialTransport(cfg DialConfig) Transport { return &dialTransport{cfg: cfg} }

// RunOnce dials the gateway, opens the Connect stream, calls the SessionRunner,
// and returns when the session ends. The gRPC connection is closed on return.
//
// connectCtx inherits ctx so caller cancellation (e.g. agent shutdown)
// propagates into the in-flight Connect RPC. If DialConfig.ConnectTimeout
// is set, an AfterFunc on the same ctx enforces the dial-side deadline.
func (t *dialTransport) RunOnce(ctx context.Context, run SessionRunner) error {
	conn, err := dialGateway(ctx, t.cfg.GatewayAddr, t.cfg.ServerName, t.cfg.CAPEM, &t.cfg.Cert)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := gatewayrpc.NewAgentGatewayClient(conn)

	// connectCtx inherits the caller's ctx so agent shutdown propagates into
	// the in-flight Connect call. If ConnectTimeout is set, an AfterFunc cancels
	// the same ctx to enforce the dial-side deadline. The cancel is invoked via
	// defer to release the context once RunOnce returns; the stream itself is
	// bound to this ctx, so its lifetime is tied to RunOnce's return.
	connectCtx, cancelConnect := context.WithCancel(ctx)
	defer cancelConnect()

	if t.cfg.ConnectTimeout > 0 {
		setupTimer := time.AfterFunc(t.cfg.ConnectTimeout, cancelConnect)
		defer setupTimer.Stop()
	}

	stream, err := client.Connect(connectCtx)
	if err != nil {
		return err
	}

	return run(ctx, stream, client)
}

// dialGateway establishes a gRPC client connection to the panel's AgentGateway
// endpoint using mutual TLS. Moved from cmd/agent/main.go.
//
// TODO(Task-17): panvex_agent_tls_handshake_failures_total — a counter for
// TLS handshake failures (CA-pin mismatch, panel-CN mismatch, etc.) cannot be
// added here yet because the agent binary does not export a Prometheus /metrics
// endpoint. A Prometheus registry would need to be threaded through the entire
// agent startup path (cmd/agent/main.go → Transport) before this counter can
// be incremented. Deferred to a follow-up observability task.
func dialGateway(ctx context.Context, gatewayAddr, serverName, caPEM string, certificate *tls.Certificate) (*grpc.ClientConn, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, errors.New("failed to append control-plane CA")
	}

	tlsConfig := &tls.Config{
		RootCAs:    pool,
		ServerName: serverName,
		MinVersion: tls.VersionTLS13,
	}
	if certificate != nil {
		tlsConfig.Certificates = []tls.Certificate{*certificate}
	}

	return grpc.NewClient(gatewayAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                dialKeepaliveTime,
			Timeout:             dialKeepaliveTimeout,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(dialMaxMessageSize),
			grpc.MaxCallSendMsgSize(dialMaxMessageSize),
		),
		// Attach x-request-id to outgoing metadata when callers have
		// seeded the call ctx via WithRequestID, so the panel can
		// correlate agent-side logs with the originating HTTP request.
		grpc.WithChainUnaryInterceptor(RequestIDUnaryInterceptor()),
		grpc.WithChainStreamInterceptor(RequestIDStreamInterceptor()),
	)
}
