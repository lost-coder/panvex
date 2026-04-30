package bootstrap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/dbsqlc"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// enrollFakeQueries is a map-backed fake satisfying EnrollQueries. It lets
// tests pre-seed rows and assert state transitions via the BootstrapState field.
type enrollFakeQueries struct {
	rows map[string]*dbsqlc.GetAgentTransportRow
}

func newEnrollFakeQueries(rows ...*dbsqlc.GetAgentTransportRow) *enrollFakeQueries {
	m := &enrollFakeQueries{rows: make(map[string]*dbsqlc.GetAgentTransportRow)}
	for _, r := range rows {
		m.rows[r.ID] = r
	}
	return m
}

func (f *enrollFakeQueries) GetAgentTransport(_ context.Context, id string) (dbsqlc.GetAgentTransportRow, error) {
	r, ok := f.rows[id]
	if !ok {
		return dbsqlc.GetAgentTransportRow{}, sql.ErrNoRows
	}
	return *r, nil
}

func (f *enrollFakeQueries) ExpireAgentBootstrapToken(_ context.Context, id string) error {
	if r, ok := f.rows[id]; ok {
		r.BootstrapState = "expired"
	}
	return nil
}

func (f *enrollFakeQueries) ClearAgentBootstrapToken(_ context.Context, id string) error {
	if r, ok := f.rows[id]; ok {
		r.BootstrapState = "active"
		r.BootstrapTokenHash = nil
		r.BootstrapExpiresAt = sql.NullTime{}
	}
	return nil
}

// fakeCA implements CertificateAuthority, recording SignCSR inputs and
// returning canned values. No real crypto.
type fakeCA struct {
	lastCSRPEM  string
	lastAgentID string
}

func (c *fakeCA) SignCSR(csrPEM, agentID string, _ time.Duration) (string, string, time.Time, error) {
	c.lastCSRPEM = csrPEM
	c.lastAgentID = agentID
	return "CERT", "CA", time.Unix(9999999999, 0), nil
}

// ---------------------------------------------------------------------------
// Stub gRPC server (agent-side)
// ---------------------------------------------------------------------------

// agentEnrollStubServer registers AgentGateway on a test gRPC server and
// handles EnrollOutbound. It sends a configured EnrollOpening, receives the
// EnrollCertificate, and stores it. Tests configure the token/agentID/CSR
// fields before starting the server.
type agentEnrollStubServer struct {
	gatewayrpc.UnimplementedAgentGatewayServer

	// fields to send in the opening; set before starting
	bootstrapToken string
	agentID        string
	csrPEM         string

	// filled in after a successful exchange
	receivedCert *gatewayrpc.EnrollCertificate

	gs      *grpc.Server
	address string
}

func (s *agentEnrollStubServer) EnrollOutbound(stream gatewayrpc.AgentGateway_EnrollOutboundServer) error {
	if err := stream.Send(&gatewayrpc.EnrollServerMessage{
		Body: &gatewayrpc.EnrollServerMessage_Opening{
			Opening: &gatewayrpc.EnrollOpening{
				BootstrapToken: s.bootstrapToken,
				AgentId:        s.agentID,
				CsrPem:         s.csrPEM,
			},
		},
	}); err != nil {
		return err
	}
	msg, err := stream.Recv()
	if err != nil {
		return err
	}
	s.receivedCert = msg.GetCertificate()
	// Return nil to close the server-side stream (signals success to panel).
	return nil
}

// startAgentEnrollStub starts a real gRPC server with TLS, registering the
// stub as the AgentGateway. The returned *tls.Config is suitable for the
// panel (gRPC client) to dial. The caller must call t.Cleanup(gs.GracefulStop).
func startAgentEnrollStub(t *testing.T, stub *agentEnrollStubServer) *tls.Config {
	t.Helper()

	caCert, caKey := mustEnrollCA(t)
	serverCert, serverKey := mustEnrollLeaf(t, caCert, caKey, "localhost")

	tlsCert := tls.Certificate{
		Certificate: [][]byte{serverCert.Raw},
		PrivateKey:  serverKey,
	}
	serverTLS := &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	clientTLS := &tls.Config{
		ServerName: "localhost",
		RootCAs:    enrollRootPool(caCert),
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	gs := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	stub.gs = gs
	stub.address = lis.Addr().String()

	gatewayrpc.RegisterAgentGatewayServer(gs, stub)
	go gs.Serve(lis) //nolint:errcheck
	t.Cleanup(gs.GracefulStop)

	return clientTLS
}

// ---------------------------------------------------------------------------
// TLS helpers (duplicated from agenttransport/outbound_test.go)
// ---------------------------------------------------------------------------

func mustEnrollCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
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
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert, priv
}

func mustEnrollLeaf(t *testing.T, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, host string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
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
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert, priv
}

func enrollRootPool(cert *x509.Certificate) *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(cert)
	return p
}

// ---------------------------------------------------------------------------
// Token helper
// ---------------------------------------------------------------------------

// mustIssueToken issues a fresh bootstrap token valid for ttl from now.
func mustIssueToken(t *testing.T, ttl time.Duration) TokenIssued {
	t.Helper()
	tok, err := IssueToken(time.Now(), ttl)
	require.NoError(t, err)
	return tok
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestEnrollDriverHappyPath(t *testing.T) {
	tok := mustIssueToken(t, time.Hour)
	agentID := "agent-happy"

	row := &dbsqlc.GetAgentTransportRow{
		ID:                 agentID,
		BootstrapState:     "pending",
		BootstrapTokenHash: tok.Hash[:],
		BootstrapExpiresAt: sql.NullTime{Time: tok.ExpiresAt, Valid: true},
	}
	fq := newEnrollFakeQueries(row)
	ca := &fakeCA{}

	stub := &agentEnrollStubServer{
		bootstrapToken: tok.Raw,
		agentID:        agentID,
		csrPEM:         "fake-csr",
	}
	clientTLS := startAgentEnrollStub(t, stub)

	driver := NewEnrollDriver(fq, ca, nil, time.Now)
	err := driver.Run(context.Background(), stub.address, clientTLS, agentID)
	require.NoError(t, err)

	// Certificate was sent to the agent.
	require.NotNil(t, stub.receivedCert)
	require.Equal(t, "CERT", stub.receivedCert.CertificatePem)
	require.Equal(t, "CA", stub.receivedCert.CaPem)

	// fakeCA received correct inputs.
	require.Equal(t, "fake-csr", ca.lastCSRPEM)
	require.Equal(t, agentID, ca.lastAgentID)

	// Bootstrap state cleared.
	require.Equal(t, "active", fq.rows[agentID].BootstrapState)
}

func TestEnrollDriverRejectsExpiredToken(t *testing.T) {
	tok := mustIssueToken(t, time.Hour)
	agentID := "agent-expired"

	row := &dbsqlc.GetAgentTransportRow{
		ID:                 agentID,
		BootstrapState:     "pending",
		BootstrapTokenHash: tok.Hash[:],
		// expires_at is in the past from the driver's perspective
		BootstrapExpiresAt: sql.NullTime{Time: time.Now().Add(-time.Minute), Valid: true},
	}
	fq := newEnrollFakeQueries(row)
	ca := &fakeCA{}

	stub := &agentEnrollStubServer{
		bootstrapToken: tok.Raw,
		agentID:        agentID,
		csrPEM:         "fake-csr",
	}
	clientTLS := startAgentEnrollStub(t, stub)

	driver := NewEnrollDriver(fq, ca, nil, time.Now)
	err := driver.Run(context.Background(), stub.address, clientTLS, agentID)
	require.ErrorIs(t, err, ErrBootstrapTokenExpired)

	// State transitioned to expired.
	require.Equal(t, "expired", fq.rows[agentID].BootstrapState)
}

func TestEnrollDriverRejectsTokenMismatch(t *testing.T) {
	tok := mustIssueToken(t, time.Hour)
	agentID := "agent-mismatch"

	row := &dbsqlc.GetAgentTransportRow{
		ID:                 agentID,
		BootstrapState:     "pending",
		BootstrapTokenHash: tok.Hash[:],
		BootstrapExpiresAt: sql.NullTime{Time: tok.ExpiresAt, Valid: true},
	}
	fq := newEnrollFakeQueries(row)
	ca := &fakeCA{}

	// Agent sends a different raw token (wrong secret).
	wrongTok := mustIssueToken(t, time.Hour)
	stub := &agentEnrollStubServer{
		bootstrapToken: wrongTok.Raw,
		agentID:        agentID,
		csrPEM:         "fake-csr",
	}
	clientTLS := startAgentEnrollStub(t, stub)

	driver := NewEnrollDriver(fq, ca, nil, time.Now)
	err := driver.Run(context.Background(), stub.address, clientTLS, agentID)
	require.ErrorIs(t, err, ErrBootstrapTokenMismatch)

	// State remains pending.
	require.Equal(t, "pending", fq.rows[agentID].BootstrapState)
}

func TestEnrollDriverRejectsAgentIDMismatch(t *testing.T) {
	tok := mustIssueToken(t, time.Hour)
	agentID := "agent-id-mismatch"

	row := &dbsqlc.GetAgentTransportRow{
		ID:                 agentID,
		BootstrapState:     "pending",
		BootstrapTokenHash: tok.Hash[:],
		BootstrapExpiresAt: sql.NullTime{Time: tok.ExpiresAt, Valid: true},
	}
	fq := newEnrollFakeQueries(row)
	ca := &fakeCA{}

	// Agent claims to be a different agent_id.
	stub := &agentEnrollStubServer{
		bootstrapToken: tok.Raw,
		agentID:        "evil-agent",
		csrPEM:         "fake-csr",
	}
	clientTLS := startAgentEnrollStub(t, stub)

	driver := NewEnrollDriver(fq, ca, nil, time.Now)
	err := driver.Run(context.Background(), stub.address, clientTLS, agentID)
	require.ErrorIs(t, err, ErrAgentIDMismatch)

	// State remains pending.
	require.Equal(t, "pending", fq.rows[agentID].BootstrapState)
}

func TestEnrollDriverFailsPreconditionWhenNotPending(t *testing.T) {
	agentID := "agent-active"

	row := &dbsqlc.GetAgentTransportRow{
		ID:             agentID,
		BootstrapState: "active",
	}
	fq := newEnrollFakeQueries(row)
	ca := &fakeCA{}

	driver := NewEnrollDriver(fq, ca, nil, time.Now)
	// No real gRPC server needed; the driver should fail before dialing.
	err := driver.Run(context.Background(), "127.0.0.1:1", nil, agentID)
	require.Error(t, err)
	// Should be a gRPC FailedPrecondition status.
	require.Contains(t, err.Error(), "bootstrap_state=active")
}
