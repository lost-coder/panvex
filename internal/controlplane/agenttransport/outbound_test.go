package agenttransport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
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
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	go sup.run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for connectCount.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := connectCount.Load(); got < 2 {
		t.Fatalf("expected >= 2 connects, got %d", got)
	}
}

// TestOutboundSupervisorEnrollsWhenPending verifies that when
// bootstrapStateFn returns "pending" the enrollFn is called before the normal
// mTLS dial, and that after enrollment succeeds (bootstrapStateFn switches to
// "active") subsequent iterations skip the enrollment step.
func TestOutboundSupervisorEnrollsWhenPending(t *testing.T) {
	stub := newAgentStubServer(t)
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var enrollCalls atomic.Int32
	var connectCalls atomic.Int32

	// Simulate state: first call returns "pending", then "active".
	callCount := atomic.Int32{}
	bootstrapStateFn := func(_ context.Context, _ string) (string, error) {
		if callCount.Add(1) == 1 {
			return "pending", nil
		}
		return "active", nil
	}
	enrollFn := func(_ context.Context, _, _ string) error {
		enrollCalls.Add(1)
		return nil
	}
	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCalls.Add(1)
		// End session immediately; supervisor reconnects.
		return nil
	}

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-1", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.Default(),
	)
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	sup.enrollFn = enrollFn
	sup.bootstrapStateFn = bootstrapStateFn

	go sup.run(ctx)

	deadline := time.Now().Add(3 * time.Second)
	// Wait until we have seen at least one enroll and at least two connects.
	for (enrollCalls.Load() < 1 || connectCalls.Load() < 2) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	if got := enrollCalls.Load(); got != 1 {
		t.Errorf("enrollFn calls: got %d, want 1", got)
	}
	if got := connectCalls.Load(); got < 2 {
		t.Errorf("connect calls: got %d, want >= 2", got)
	}
}

// TestOutboundSupervisorRetriesAfterEnrollFailure verifies that when enrollFn
// returns an error the supervisor backs off and tries again, and that once
// enrollFn eventually succeeds the normal mTLS dial is reached. This guards
// the failure-then-recover path that the happy-path test does not exercise.
func TestOutboundSupervisorRetriesAfterEnrollFailure(t *testing.T) {
	stub := newAgentStubServer(t)
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var enrollCalls atomic.Int32
	var connectCalls atomic.Int32

	// Stay "pending" until enrollFn succeeds, then flip to "active" so the
	// supervisor stops trying to enroll and proceeds to the mTLS dial.
	state := atomic.Value{}
	state.Store("pending")
	bootstrapStateFn := func(_ context.Context, _ string) (string, error) {
		return state.Load().(string), nil
	}
	// First two calls fail; third succeeds and flips the bootstrap state.
	enrollFn := func(_ context.Context, _, _ string) error {
		n := enrollCalls.Add(1)
		if n < 3 {
			return errors.New("enroll: simulated transient failure")
		}
		state.Store("active")
		return nil
	}
	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCalls.Add(1)
		return nil
	}

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-1", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.Default(),
	)
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	sup.enrollFn = enrollFn
	sup.bootstrapStateFn = bootstrapStateFn

	go sup.run(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for (enrollCalls.Load() < 3 || connectCalls.Load() < 1) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	if got := enrollCalls.Load(); got < 3 {
		t.Errorf("enrollFn calls: got %d, want >= 3 (two failures + one success)", got)
	}
	if got := connectCalls.Load(); got < 1 {
		t.Errorf("connect calls: got %d, want >= 1 (mTLS dial after successful enrollment)", got)
	}
}

// TestOutboundSupervisorSkipsEnrollWhenActive verifies that enrollFn is never
// called when bootstrapStateFn consistently returns "active".
func TestOutboundSupervisorSkipsEnrollWhenActive(t *testing.T) {
	stub := newAgentStubServer(t)
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var enrollCalls atomic.Int32
	var connectCalls atomic.Int32

	bootstrapStateFn := func(_ context.Context, _ string) (string, error) {
		return "active", nil
	}
	enrollFn := func(_ context.Context, _, _ string) error {
		enrollCalls.Add(1)
		return nil
	}
	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCalls.Add(1)
		return nil
	}

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-1", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.Default(),
	)
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	sup.enrollFn = enrollFn
	sup.bootstrapStateFn = bootstrapStateFn

	go sup.run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for connectCalls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	if got := enrollCalls.Load(); got != 0 {
		t.Errorf("enrollFn should not be called when state=active, got %d calls", got)
	}
}

// TestOutboundTransportSupervisorGaugeDelta verifies that the
// onSupervisorDelta callback fires with the right deltas as supervisors are
// added and removed, without needing a real gRPC server.
func TestOutboundTransportSupervisorGaugeDelta(t *testing.T) {
	var total int64
	delta := func(d float64) { total += int64(d) }

	ot := newOutboundTransport(nil, nil, slog.Default())
	ot.onSupervisorDelta = delta

	// Add two supervisors. Their goroutines will loop on connectAndServe
	// and fail immediately (tlsCfg==nil → errOutboundTLSMissing), but that
	// only affects the goroutines — the delta callback fires before they run.
	ot.ensureSupervisor(NodeMeta{NodeID: "n1", AgentID: "a1", DialAddress: "127.0.0.1:1"})
	ot.ensureSupervisor(NodeMeta{NodeID: "n2", AgentID: "a2", DialAddress: "127.0.0.1:2"})

	if total != 2 {
		t.Fatalf("after 2 ensureSupervisor: total=%d, want 2", total)
	}

	ot.removeSupervisor("n1")
	if total != 1 {
		t.Fatalf("after removeSupervisor(n1): total=%d, want 1", total)
	}

	ot.stopAll()
	if total != 0 {
		t.Fatalf("after stopAll: total=%d, want 0", total)
	}
}

// TestOutboundSupervisorUsesBackoffGetters verifies that backoffInitialFn /
// backoffMaxFn are consulted on each reconnect iteration rather than using
// the package-level constants. The test wires getter functions that return
// very short durations (so the supervisor reconnects quickly in CI), then
// confirms multiple connects occur within the deadline, proving the getter
// path drives the backoff.
func TestOutboundSupervisorUsesBackoffGetters(t *testing.T) {
	stub := newAgentStubServer(t)
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var connectCount atomic.Int32
	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCount.Add(1)
		return nil
	}

	// Values deliberately different from the package constants to prove the
	// getter path is exercised.
	const wantInitial = 5 * time.Millisecond
	const wantMax = 20 * time.Millisecond

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-getter", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.Default(),
	)
	sup.backoffInitialFn = func() time.Duration { return wantInitial }
	sup.backoffMaxFn = func() time.Duration { return wantMax }
	go sup.run(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for connectCount.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := connectCount.Load(); got < 3 {
		t.Fatalf("expected >= 3 connects via getter-driven backoff, got %d", got)
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

	var lc net.ListenConfig
	lis, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
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
