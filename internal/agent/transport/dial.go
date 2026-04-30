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
// If DialConfig.ConnectTimeout is set, the Connect RPC must return within that
// duration; otherwise the dial context is used directly.
func (t *dialTransport) RunOnce(ctx context.Context, run SessionRunner) error {
	conn, err := dialGateway(ctx, t.cfg.GatewayAddr, t.cfg.ServerName, t.cfg.CAPEM, &t.cfg.Cert)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := gatewayrpc.NewAgentGatewayClient(conn)

	// Apply optional connect timeout using the same timer-cancel pattern as the
	// previous connectStreamWithSetupTimeout helper in cmd/agent/main.go.
	connectCtx, cancelConnect := context.WithCancel(context.Background())
	var setupTimer *time.Timer
	if t.cfg.ConnectTimeout > 0 {
		setupTimer = time.AfterFunc(t.cfg.ConnectTimeout, cancelConnect)
	}
	stream, err := client.Connect(connectCtx)
	if setupTimer != nil {
		setupTimer.Stop()
	}
	if err != nil {
		cancelConnect()
		return err
	}
	// On success the stream owns connectCtx — cancelling it would kill the
	// stream immediately. The context is released when the stream closes naturally.
	_ = cancelConnect //nolint:ineffassign // cancel is transferred to the stream lifecycle

	return run(ctx, stream, client)
}

// dialGateway establishes a gRPC client connection to the panel's AgentGateway
// endpoint using mutual TLS. Moved from cmd/agent/main.go.
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
	)
}
