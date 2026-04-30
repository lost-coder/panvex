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

	lis, err := net.Listen("tcp", "127.0.0.1:0")
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

// ---- TLS helpers (mirrors internal/controlplane/agenttransport/outbound_test.go) ----

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
