package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Compile-time check: *dialTransport must satisfy Transport.
var _ Transport = (*dialTransport)(nil)

// TestDialTransportRunOnce spins up an in-process gRPC server with a stub
// AgentGateway, then verifies that RunOnce dials, delivers the stream to the
// SessionRunner, and returns cleanly when the runner exits.
func TestDialTransportRunOnce(t *testing.T) {
	stub := newStubServer(t)
	defer stub.close()

	// Encode CA PEM for DialConfig.
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: stub.caCert.Raw})

	cfg := DialConfig{
		GatewayAddr: stub.address,
		ServerName:  "localhost",
		CAPEM:       string(caPEM),
		Cert:        stub.clientCert,
	}

	runnerCalled := false
	runner := SessionRunner(func(ctx context.Context, stream BidiStream, client gatewayrpc.AgentGatewayClient) error {
		runnerCalled = true
		_ = client // client is available for unary RPCs like RenewCertificate
		return nil
	})

	tr := NewDialTransport(cfg)
	err := tr.RunOnce(context.Background(), runner)
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if !runnerCalled {
		t.Fatal("SessionRunner was not called")
	}
}

// ---- stub server ----

type stubServer struct {
	server     *grpc.Server
	listener   net.Listener
	address    string
	caCert     *x509.Certificate
	clientCert tls.Certificate
	gatewayrpc.UnimplementedAgentGatewayServer
}

func (s *stubServer) Connect(stream gatewayrpc.AgentGateway_ConnectServer) error {
	// Accept the connection and return EOF immediately so the runner returns.
	return nil
}

func (s *stubServer) close() { s.server.GracefulStop() }

func newStubServer(t *testing.T) *stubServer {
	t.Helper()
	caCert, caKey := mustGenerateCA(t)
	serverCert, serverKey := mustGenerateLeaf(t, caCert, caKey, "localhost")
	clientCert, clientKey := mustGenerateLeaf(t, caCert, caKey, "agent")

	serverTLSCert := tls.Certificate{
		Certificate: [][]byte{serverCert.Raw},
		PrivateKey:  serverKey,
	}
	clientTLSCert := tls.Certificate{
		Certificate: [][]byte{clientCert.Raw},
		PrivateKey:  clientKey,
	}

	// Server requires client cert signed by CA.
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)
	serverTLSCfg := &tls.Config{
		Certificates: []tls.Certificate{serverTLSCert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	var lc net.ListenConfig
	lis, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	gs := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLSCfg)))
	stub := &stubServer{
		server:     gs,
		listener:   lis,
		address:    lis.Addr().String(),
		caCert:     caCert,
		clientCert: clientTLSCert,
	}
	gatewayrpc.RegisterAgentGatewayServer(gs, stub)
	go gs.Serve(lis) //nolint:errcheck
	return stub
}

// TestDialTransportRunOnceCancelsConnectViaParentCtx verifies that when the
// caller's ctx is cancelled, the in-flight Connect RPC unwinds promptly rather
// than waiting for ConnectTimeout. Regression for A-3: connectCtx previously
// descended from context.Background(), divorcing it from agent shutdown.
func TestDialTransportRunOnceCancelsConnectViaParentCtx(t *testing.T) {
	// Use a never-accepting listener so the Connect RPC blocks until cancel.
	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	// CA cert that the dial config trusts; we deliberately do NOT run a gRPC
	// server, so client.Connect blocks on the TLS handshake.
	caCert, _ := mustGenerateCA(t)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})

	cfg := DialConfig{
		GatewayAddr:    listener.Addr().String(),
		ServerName:     "nope",
		CAPEM:          string(caPEM),
		ConnectTimeout: 5 * time.Second, // larger than the test deadline below
	}

	tr := NewDialTransport(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.RunOnce(ctx, func(_ context.Context, _ BidiStream, _ gatewayrpc.AgentGatewayClient) error {
			return nil
		})
	}()

	// Cancel the parent ctx; RunOnce should unwind quickly, NOT wait the
	// full 5s ConnectTimeout. If connectCtx still descended from
	// context.Background(), this test would time out.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-errCh:
		// ok — returned promptly after parent cancel
	case <-time.After(2 * time.Second):
		t.Fatal("RunOnce did not honour parent ctx cancel")
	}
}

// TestDialTransportConnectTimeoutDoesNotCancelLiveStream verifies that the
// ConnectTimeout AfterFunc fires only against the dial-setup phase: once
// client.Connect returns a stream, the timer must be stopped so it cannot
// later cancel connectCtx mid-session. Regression for the bug where
// setupTimer.Stop() was deferred — the timer kept running through the
// long-lived stream and cancelled the inherited stream ctx after the
// ConnectTimeout window expired.
//
// Setup: a real in-process gRPC server that holds the Connect stream open,
// a 50ms ConnectTimeout, and a runner that calls stream.Recv() (which blocks
// on the connectCtx-derived stream context). With the bug the 50ms timer
// fires while the runner is blocked in Recv(), cancels connectCtx, and Recv
// returns a context-cancelled error. With the fix the timer is stopped once
// Connect returns and the server's eventual graceful close drives Recv to
// return io.EOF cleanly.
func TestDialTransportConnectTimeoutDoesNotCancelLiveStream(t *testing.T) {
	stub := newBlockingStubServer(t)
	defer stub.close()

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: stub.caCert.Raw})

	cfg := DialConfig{
		GatewayAddr:    stub.address,
		ServerName:     "localhost",
		CAPEM:          string(caPEM),
		Cert:           stub.clientCert,
		ConnectTimeout: 50 * time.Millisecond,
	}

	runnerEntered := make(chan struct{})
	runnerErr := make(chan error, 1)
	runner := SessionRunner(func(_ context.Context, stream BidiStream, _ gatewayrpc.AgentGatewayClient) error {
		close(runnerEntered)
		// Block in Recv for ~250ms — longer than the 50ms ConnectTimeout.
		// On the buggy code, the timer cancels connectCtx mid-flight and
		// stream.Recv returns a context-cancelled error. On the fixed
		// code, Recv stays blocked until we release the server.
		_, err := stream.Recv()
		runnerErr <- err
		return err
	})

	tr := NewDialTransport(cfg)
	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.RunOnce(context.Background(), runner)
	}()

	select {
	case <-runnerEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not start within 2s")
	}

	// Wait well past the ConnectTimeout. With the bug, the timer fires at
	// ~50ms and the runner's Recv returns immediately with context.Canceled.
	// With the fix, Recv is still blocked here.
	select {
	case err := <-runnerErr:
		t.Fatalf("runner returned during live stream (timer fired mid-session): err=%v", err)
	case <-time.After(250 * time.Millisecond):
		// good — Recv is still blocked, timer did not cancel the stream
	}

	// Release the server so Recv unblocks and RunOnce returns.
	stub.release()

	select {
	case <-errCh:
		// fine — Recv returned (likely io.EOF) and RunOnce unwound
	case <-time.After(3 * time.Second):
		t.Fatal("RunOnce did not return within 3s after server release")
	}
}

// blockingStubServer is a variant of stubServer whose Connect handler holds
// the stream open until release() is called, so the agent-side stream.Recv()
// stays blocked instead of returning EOF immediately.
type blockingStubServer struct {
	*stubServer
	releaseCh chan struct{}
}

func (s *blockingStubServer) Connect(stream gatewayrpc.AgentGateway_ConnectServer) error {
	select {
	case <-s.releaseCh:
		return nil
	case <-stream.Context().Done():
		return stream.Context().Err()
	}
}

func (s *blockingStubServer) release() { close(s.releaseCh) }

func newBlockingStubServer(t *testing.T) *blockingStubServer {
	t.Helper()
	caCert, caKey := mustGenerateCA(t)
	serverCert, serverKey := mustGenerateLeaf(t, caCert, caKey, "localhost")
	clientCert, clientKey := mustGenerateLeaf(t, caCert, caKey, "agent")

	serverTLSCert := tls.Certificate{
		Certificate: [][]byte{serverCert.Raw},
		PrivateKey:  serverKey,
	}
	clientTLSCert := tls.Certificate{
		Certificate: [][]byte{clientCert.Raw},
		PrivateKey:  clientKey,
	}

	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)
	serverTLSCfg := &tls.Config{
		Certificates: []tls.Certificate{serverTLSCert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	var lc net.ListenConfig
	lis, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	gs := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLSCfg)))
	inner := &stubServer{
		server:     gs,
		listener:   lis,
		address:    lis.Addr().String(),
		caCert:     caCert,
		clientCert: clientTLSCert,
	}
	stub := &blockingStubServer{stubServer: inner, releaseCh: make(chan struct{})}
	gatewayrpc.RegisterAgentGatewayServer(gs, stub)
	go gs.Serve(lis) //nolint:errcheck
	return stub
}

// ---- listen transport test ----

func TestListenTransportAcceptsIncomingStream(t *testing.T) {
	caCert, caKey := mustGenerateCA(t)
	agentCert, agentKey := mustGenerateLeaf(t, caCert, caKey, "127.0.0.1")
	panelCert, panelKey := mustGenerateLeaf(t, caCert, caKey, "panel.test")

	cfg := ListenConfig{
		Addr: "127.0.0.1:0",
		Cert: tls.Certificate{
			Certificate: [][]byte{agentCert.Raw},
			PrivateKey:  agentKey,
		},
		CAPEM: encodeCertPEM(caCert),
	}
	tr := NewListenTransport(cfg).(*listenTransport)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() {
		done <- tr.RunOnce(ctx, func(_ context.Context, stream BidiStream, _ gatewayrpc.AgentGatewayClient) error {
			// Receive the panel's first message.
			_, err := stream.Recv()
			return err
		})
	}()

	// Wait for the listener to bind (Address becomes non-empty).
	deadline := time.Now().Add(time.Second)
	for tr.Address() == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if tr.Address() == "" {
		t.Fatal("listener never bound")
	}

	// Dial in as the panel: gRPC client uses the panel's leaf cert as its
	// client cert; trusts the agent's cert via the same CA.
	clientTLS := &tls.Config{
		ServerName:   "127.0.0.1",
		RootCAs:      certPoolFromCert(caCert),
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{panelCert.Raw},
			PrivateKey:  panelKey,
		}},
	}
	conn, err := grpc.NewClient(tr.Address(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	client := gatewayrpc.NewAgentGatewayClient(conn)
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	// Send a ConnectServerMessage on the wire using untyped SendMsg —
	// the gRPC client stream is natively typed for ConnectClientMessage but in
	// reverse mode the panel sends ConnectServerMessage.
	if err := stream.SendMsg(&gatewayrpc.ConnectServerMessage{}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("listen runner returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listen runner didn't return within 2s")
	}
}

// ---- TLS helpers (mirrors internal/controlplane/agenttransport/outbound_test.go) ----

func encodeCertPEM(cert *x509.Certificate) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
}

func certPoolFromCert(cert *x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return pool
}

func mustGenerateCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("ca create: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ca parse: %v", err)
	}
	return cert, priv
}

// TestListenTransportRunOnceNoGoroutineLeak asserts that the GracefulStop
// helper goroutine inside RunOnce is scoped to the call and does not survive
// after RunOnce returns. Without the fix, each reconnect cycle leaked one
// goroutine until the outer ctx was cancelled.
//
// The leak only manifests when RunOnce returns via the `done` or `serveErr`
// branches *while the caller's ctx is still alive* — that is, when the runner
// finishes naturally (one accepted stream completed) before any cancel.
// We simulate this by dialing in as a panel, sending one frame, closing send,
// and letting the runner return. The parent ctx is kept alive across all
// iterations so any leaked goroutine stays parked.
func TestListenTransportRunOnceNoGoroutineLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("goroutine leak test is slow")
	}

	caCert, caKey := mustGenerateCA(t)
	agentCert, agentKey := mustGenerateLeaf(t, caCert, caKey, "127.0.0.1")
	panelCert, panelKey := mustGenerateLeaf(t, caCert, caKey, "panel.test")
	caPEM := encodeCertPEM(caCert)

	cfg := ListenConfig{
		Addr: "127.0.0.1:0",
		Cert: tls.Certificate{
			Certificate: [][]byte{agentCert.Raw},
			PrivateKey:  agentKey,
		},
		CAPEM: caPEM,
	}

	clientTLS := &tls.Config{
		ServerName: "127.0.0.1",
		RootCAs:    certPoolFromCert(caCert),
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{panelCert.Raw},
			PrivateKey:  panelKey,
		}},
	}

	// Parent ctx must stay alive across all iterations — that is what keeps
	// any leaked `<-ctx.Done(); GracefulStop()` goroutine parked.
	parentCtx, parentCancel := context.WithCancel(context.Background())
	t.Cleanup(parentCancel)

	runOnceCycle := func() {
		tr := NewListenTransport(cfg).(*listenTransport)
		done := make(chan error, 1)
		go func() {
			done <- tr.RunOnce(parentCtx, func(_ context.Context, stream BidiStream, _ gatewayrpc.AgentGatewayClient) error {
				_, err := stream.Recv()
				return err
			})
		}()

		// Wait for the listener to bind.
		deadline := time.Now().Add(2 * time.Second)
		for tr.Address() == "" && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if tr.Address() == "" {
			t.Fatal("listener never bound")
		}

		conn, err := grpc.NewClient(tr.Address(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		client := gatewayrpc.NewAgentGatewayClient(conn)
		stream, err := client.Connect(parentCtx)
		if err != nil {
			conn.Close()
			t.Fatalf("client.Connect: %v", err)
		}
		if err := stream.SendMsg(&gatewayrpc.ConnectServerMessage{}); err != nil {
			conn.Close()
			t.Fatalf("send: %v", err)
		}
		_ = stream.CloseSend()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			conn.Close()
			t.Fatal("RunOnce did not return within 3s")
		}
		conn.Close()
	}

	// Warm-up: one full cycle so gRPC's internal pools spawn their permanent
	// goroutines; baseline reflects steady state.
	runOnceCycle()
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	baseline := runtime.NumGoroutine()

	const iters = 20
	for i := 0; i < iters; i++ {
		runOnceCycle()
	}
	time.Sleep(300 * time.Millisecond)
	runtime.GC()
	final := runtime.NumGoroutine()

	if final > baseline+5 {
		t.Fatalf("goroutine leak: baseline=%d final=%d (delta %d > 5 over %d iterations)",
			baseline, final, final-baseline, iters)
	}
}

func mustGenerateLeaf(t *testing.T, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, host string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &priv.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("leaf create: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("leaf parse: %v", err)
	}
	return cert, priv
}
