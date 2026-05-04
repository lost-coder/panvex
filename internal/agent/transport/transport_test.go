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
