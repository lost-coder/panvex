package bootstrap

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"errors"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
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

// ---------------------------------------------------------------------------
// fakeCertPinWriter implements CertPinWriter for tests.
// ---------------------------------------------------------------------------

type fakeCertPinWriter struct {
	knownAgents map[string]struct{} // agents that exist; UpdateAgentCertPin returns ErrNotFound for others
	pins        map[string][]byte   // agentID → SPKI SHA-256 pin
}

// newFakeCertPinWriter creates a fakeCertPinWriter that accepts updates for the
// given set of agentIDs and returns storage.ErrNotFound for all others.
func newFakeCertPinWriter(agentIDs ...string) *fakeCertPinWriter {
	f := &fakeCertPinWriter{
		knownAgents: make(map[string]struct{}),
		pins:        make(map[string][]byte),
	}
	for _, id := range agentIDs {
		f.knownAgents[id] = struct{}{}
	}
	return f
}

func (f *fakeCertPinWriter) UpdateAgentCertPin(_ context.Context, agentID string, pin []byte) error {
	if _, ok := f.knownAgents[agentID]; !ok {
		return storage.ErrNotFound
	}
	cp := make([]byte, len(pin))
	copy(cp, pin)
	f.pins[agentID] = cp
	return nil
}

// getPin returns the stored pin for test assertions; returns nil if not set.
func (f *fakeCertPinWriter) getPin(agentID string) []byte {
	return f.pins[agentID]
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
// panel (gRPC client) to dial; the *x509.Certificate is the server's leaf cert
// (used by callers that need to compute the expected SPKI pin).
// The caller must call t.Cleanup(gs.GracefulStop).
func startAgentEnrollStub(t *testing.T, stub *agentEnrollStubServer) (*tls.Config, *x509.Certificate) {
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

	var lc net.ListenConfig
	lis, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	gs := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	stub.gs = gs
	stub.address = lis.Addr().String()

	gatewayrpc.RegisterAgentGatewayServer(gs, stub)
	go gs.Serve(lis) //nolint:errcheck
	t.Cleanup(gs.GracefulStop)

	return clientTLS, serverCert
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
	clientTLS, _ := startAgentEnrollStub(t, stub)

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
	clientTLS, _ := startAgentEnrollStub(t, stub)

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
	clientTLS, _ := startAgentEnrollStub(t, stub)

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
	clientTLS, _ := startAgentEnrollStub(t, stub)

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

// TestEnrollDriverCallbacksOnHappyPath verifies that AttemptRecorder and
// EventNotifier are both called with the correct values on a successful Run.
func TestEnrollDriverCallbacksOnHappyPath(t *testing.T) {
	tok := mustIssueToken(t, time.Hour)
	agentID := "agent-callbacks"

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
	clientTLS, _ := startAgentEnrollStub(t, stub)

	var recordedResult string
	var notifiedActions []string

	driver := NewEnrollDriver(fq, ca, nil, time.Now)
	driver.SetAttemptRecorder(func(result string) { recordedResult = result })
	driver.SetEventNotifier(func(action, id string) {
		notifiedActions = append(notifiedActions, action)
	})

	err := driver.Run(context.Background(), stub.address, clientTLS, agentID)
	require.NoError(t, err)

	require.Equal(t, "success", recordedResult, "AttemptRecorder must be called with 'success'")
	require.Contains(t, notifiedActions, "bootstrap.enrollment_attempted")
	require.Contains(t, notifiedActions, "bootstrap.enrollment_completed")
}

// TestEnrollDriverCallbacksOnExpiredToken verifies that AttemptRecorder is
// called with "expired" and the enrollment_expired event is emitted.
func TestEnrollDriverCallbacksOnExpiredToken(t *testing.T) {
	tok := mustIssueToken(t, time.Hour)
	agentID := "agent-cb-expired"

	row := &dbsqlc.GetAgentTransportRow{
		ID:                 agentID,
		BootstrapState:     "pending",
		BootstrapTokenHash: tok.Hash[:],
		BootstrapExpiresAt: sql.NullTime{Time: time.Now().Add(-time.Minute), Valid: true},
	}
	fq := newEnrollFakeQueries(row)
	ca := &fakeCA{}
	stub := &agentEnrollStubServer{
		bootstrapToken: tok.Raw,
		agentID:        agentID,
		csrPEM:         "fake-csr",
	}
	clientTLS, _ := startAgentEnrollStub(t, stub)

	var recordedResult string
	var notifiedActions []string

	driver := NewEnrollDriver(fq, ca, nil, time.Now)
	driver.SetAttemptRecorder(func(result string) { recordedResult = result })
	driver.SetEventNotifier(func(action, _ string) {
		notifiedActions = append(notifiedActions, action)
	})

	err := driver.Run(context.Background(), stub.address, clientTLS, agentID)
	require.ErrorIs(t, err, ErrBootstrapTokenExpired)

	require.Equal(t, "expired", recordedResult, "AttemptRecorder must be called with 'expired'")
	require.Contains(t, notifiedActions, "bootstrap.enrollment_attempted")
	require.Contains(t, notifiedActions, "bootstrap.enrollment_expired")
}

// ---------------------------------------------------------------------------
// persistCertPin unit tests (S-02)
// ---------------------------------------------------------------------------

// newSelfSignedTestCert creates a minimal self-signed x509.Certificate whose
// RawSubjectPublicKeyInfo is populated — enough to test SPKI pinning.
func newSelfSignedTestCert(t *testing.T) *x509.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "test-pin-cert"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// TestEnrollDriver_PersistsCertPinOnSuccess verifies that persistCertPin
// computes SHA-256(SPKI) and writes it via UpdateAgentCertPin (S-02).
func TestEnrollDriver_PersistsCertPinOnSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	agentID := "agent-pin-success"
	fq := newEnrollFakeQueries(&dbsqlc.GetAgentTransportRow{ID: agentID})
	pw := newFakeCertPinWriter(agentID)

	cert := newSelfSignedTestCert(t)
	expected := sha256.Sum256(cert.RawSubjectPublicKeyInfo)

	driver := NewEnrollDriver(fq, &fakeCA{}, nil, time.Now)
	driver.SetCertPinWriter(pw)
	require.NoError(t, driver.persistCertPin(ctx, agentID, cert))

	pin := pw.getPin(agentID)
	if !bytes.Equal(pin, expected[:]) {
		t.Fatalf("pin = %x, want %x", pin, expected[:])
	}
}

// TestEnrollDriver_PersistCertPin_UnknownAgent verifies that persistCertPin
// propagates ErrNotFound when the agent does not exist in storage (S-02).
func TestEnrollDriver_PersistCertPin_UnknownAgent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fq := newEnrollFakeQueries() // empty — no agents seeded
	pw := newFakeCertPinWriter() // empty — no known agents
	cert := newSelfSignedTestCert(t)

	driver := NewEnrollDriver(fq, &fakeCA{}, nil, time.Now)
	driver.SetCertPinWriter(pw)
	err := driver.persistCertPin(ctx, "no-such-agent", cert)
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("err = %v, want storage.ErrNotFound", err)
	}
}

// TestEnrollDriverHappyPath_PersistsCertPin is an end-to-end integration test
// that exercises the full Run path: the tls.Config.Clone() + VerifyConnection
// hook that captures agentLeafCert, and the call to persistCertPin that stores
// the SPKI pin. A future refactor that breaks the wiring will fail here (S-02).
func TestEnrollDriverHappyPath_PersistsCertPin(t *testing.T) {
	t.Parallel()

	tok := mustIssueToken(t, time.Hour)
	agentID := "agent-pin-e2e"

	row := &dbsqlc.GetAgentTransportRow{
		ID:                 agentID,
		BootstrapState:     "pending",
		BootstrapTokenHash: tok.Hash[:],
		BootstrapExpiresAt: sql.NullTime{Time: tok.ExpiresAt, Valid: true},
	}
	fq := newEnrollFakeQueries(row)
	pw := newFakeCertPinWriter(agentID)

	stub := &agentEnrollStubServer{
		bootstrapToken: tok.Raw,
		agentID:        agentID,
		csrPEM:         "fake-csr",
	}
	clientTLS, serverCert := startAgentEnrollStub(t, stub)

	driver := NewEnrollDriver(fq, &fakeCA{}, nil, time.Now)
	driver.SetCertPinWriter(pw)

	if err := driver.Run(context.Background(), stub.address, clientTLS, agentID); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := pw.getPin(agentID)
	if len(got) == 0 {
		t.Fatalf("pin was NOT persisted via Run path — VerifyConnection hook may not be wired into the actual handshake")
	}

	expected := sha256.Sum256(serverCert.RawSubjectPublicKeyInfo)
	if !bytes.Equal(got, expected[:]) {
		t.Fatalf("pin mismatch: got %x, want %x", got, expected[:])
	}
}

// ---------------------------------------------------------------------------
// S-02 regression tests
// ---------------------------------------------------------------------------

// TestBootstrapToken_DefaultTTLIsAtMost5Minutes locks down the S-02
// requirement that the production default bootstrap-token TTL must not exceed
// 5 minutes.  A future change that bumps installCommandTTL above 5 minutes
// will fail here immediately.
func TestBootstrapToken_DefaultTTLIsAtMost5Minutes(t *testing.T) {
	t.Parallel()
	const maxTTL = 5 * time.Minute
	if installCommandTTL > maxTTL {
		t.Fatalf("installCommandTTL = %s, want <= %s (S-02 hardening)", installCommandTTL, maxTTL)
	}
}

// TestBootstrapToken_SingleUse verifies that a bootstrap token cannot be
// consumed a second time.  After a successful enrollment, bootstrap_state
// transitions to "active" and any subsequent Run attempt is rejected with a
// FailedPrecondition error — ensuring the token is single-use (S-02).
func TestBootstrapToken_SingleUse(t *testing.T) {
	t.Parallel()

	tok := mustIssueToken(t, time.Hour)
	agentID := "agent-single-use"

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
	clientTLS, _ := startAgentEnrollStub(t, stub)

	// First Run — must succeed (token consumed, state → active).
	driver := NewEnrollDriver(fq, ca, nil, time.Now)
	if err := driver.Run(context.Background(), stub.address, clientTLS, agentID); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if got := fq.rows[agentID].BootstrapState; got != "active" {
		t.Fatalf("after first Run: BootstrapState = %q, want %q", got, "active")
	}

	// Second Run — must fail because state is now "active" (not "pending").
	// The driver returns a gRPC FailedPrecondition status wrapping the state.
	err := driver.Run(context.Background(), stub.address, clientTLS, agentID)
	if err == nil {
		t.Fatal("second Run: succeeded; expected FailedPrecondition (token is single-use, S-02)")
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("second Run: got code %s (err=%v), want FailedPrecondition", status.Code(err), err)
	}
}
