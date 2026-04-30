package agenttransport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"math/big"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func TestOutboundSupervisorReconnectsAfterDisconnect(t *testing.T) {
	stub := newAgentStubServer(t)
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var connectCount atomic.Int32
	handler := func(_ context.Context, sess AgentSession, _ NodeMeta) error {
		connectCount.Add(1)
		// End session immediately so supervisor reconnects.
		return nil
	}

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-1", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.Default(),
	)
	sup.backoffInitial = 10 * time.Millisecond
	sup.backoffMax = 50 * time.Millisecond
	go sup.run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for connectCount.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := connectCount.Load(); got < 2 {
		t.Fatalf("expected >= 2 connects, got %d", got)
	}
}

// ----------------- helpers -----------------

type agentStubServer struct {
	server    *grpc.Server
	listener  net.Listener
	address   string
	clientTLS *tls.Config
	gatewayrpc.UnimplementedAgentGatewayServer
}

// Connect is the agent-side handler in the stub. The panel (gRPC client) sends
// ConnectServerMessage on the wire using the inverted-type approach; we peek at
// one such message via RecvMsg then return so the panel sees EOF and the
// supervisor reconnects.
func (s *agentStubServer) Connect(stream gatewayrpc.AgentGateway_ConnectServer) error {
	var inbound gatewayrpc.ConnectServerMessage
	_ = stream.RecvMsg(&inbound)
	return nil
}

func (s *agentStubServer) Close() {
	s.server.GracefulStop()
}

func newAgentStubServer(t *testing.T) *agentStubServer {
	t.Helper()
	caCert, caKey := mustGenerateCA(t)
	serverCert, serverKey := mustGenerateLeaf(t, caCert, caKey, "localhost")

	tlsCert := tls.Certificate{
		Certificate: [][]byte{serverCert.Raw},
		PrivateKey:  serverKey,
	}
	serverTLSCfg := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}
	clientTLSCfg := &tls.Config{
		ServerName: "localhost",
		RootCAs:    rootPool(caCert),
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	creds := credentials.NewTLS(serverTLSCfg)
	gs := grpc.NewServer(grpc.Creds(creds))
	stub := &agentStubServer{
		server:    gs,
		listener:  lis,
		address:   lis.Addr().String(),
		clientTLS: clientTLSCfg,
	}
	gatewayrpc.RegisterAgentGatewayServer(gs, stub)
	go gs.Serve(lis) //nolint:errcheck
	return stub
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

func rootPool(cert *x509.Certificate) *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(cert)
	return p
}
