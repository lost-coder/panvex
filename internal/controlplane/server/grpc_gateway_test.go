package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/metrics"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// TestAgentInboundDropsTotalRegistered makes sure the production counter is
// wired through metrics.NewCollectors and surfaces on /metrics scrapes.
func TestAgentInboundDropsTotalRegistered(t *testing.T) {
	mc := metrics.NewCollectors()
	if mc.AgentInboundDropsTotal == nil {
		t.Fatal("AgentInboundDropsTotal is nil after metrics.NewCollectors()")
	}
	mc.AgentInboundDropsTotal.Inc()
	if got := testutil.ToFloat64(mc.AgentInboundDropsTotal); got != 1 {
		t.Fatalf("AgentInboundDropsTotal = %v, want 1", got)
	}
}

func TestServerConnectRateLimitRejectsBurstReconnects(t *testing.T) {
	currentTime := time.Date(2026, time.March, 23, 8, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return currentTime },
	})
	server.grpcConnectRateLimiter = newFixedWindowRateLimiter(1, time.Minute)

	firstStream := newFakeGatewayConnectStream(authenticatedAgentContextForTest("agent-1"))
	if err := server.Gateway().Connect(firstStream); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}

	secondStream := newFakeGatewayConnectStream(authenticatedAgentContextForTest("agent-1"))
	err := server.Gateway().Connect(secondStream)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("second Connect() code = %v, want %v", status.Code(err), codes.ResourceExhausted)
	}
}

func authenticatedAgentContextForTest(agentID string) context.Context {
	certificate := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: agentID,
		},
	}
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{
			State: tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{certificate},
			},
		},
	})
}

type fakeGatewayConnectStream struct {
	ctx  context.Context
	sent []*gatewayrpc.ConnectServerMessage
}

func newFakeGatewayConnectStream(ctx context.Context) *fakeGatewayConnectStream {
	return &fakeGatewayConnectStream{
		ctx:  ctx,
		sent: make([]*gatewayrpc.ConnectServerMessage, 0),
	}
}

func (s *fakeGatewayConnectStream) Send(message *gatewayrpc.ConnectServerMessage) error {
	s.sent = append(s.sent, message)
	return nil
}

func (s *fakeGatewayConnectStream) Recv() (*gatewayrpc.ConnectClientMessage, error) {
	return nil, io.EOF
}

func (s *fakeGatewayConnectStream) SetHeader(_ metadata.MD) error {
	return nil
}

func (s *fakeGatewayConnectStream) SendHeader(_ metadata.MD) error {
	return nil
}

func (s *fakeGatewayConnectStream) SetTrailer(_ metadata.MD) {}

func (s *fakeGatewayConnectStream) Context() context.Context {
	return s.ctx
}

func (s *fakeGatewayConnectStream) SendMsg(_ any) error {
	return nil
}

func (s *fakeGatewayConnectStream) RecvMsg(_ any) error {
	return io.EOF
}

// ---- In-stream cert renewal tests -----------------------------------------------

// fakeSendSession captures outbound ConnectServerMessages sent by the handler.
type fakeSendSession struct {
	sent []*gatewayrpc.ConnectServerMessage
}

func (s *fakeSendSession) Send(msg *gatewayrpc.ConnectServerMessage) error {
	s.sent = append(s.sent, msg)
	return nil
}

func (s *fakeSendSession) Recv() (*gatewayrpc.ConnectClientMessage, error) {
	return nil, io.EOF
}

func (s *fakeSendSession) Context() context.Context {
	return context.Background()
}

// TestHandleInStreamRenewalRequestRejectsRevokedAgent guards H-3: a revoked
// agent whose Connect stream is still alive must not be able to renew its
// certificate over the stream (which would also re-pin its serial and defeat
// the revocation + serial-pin defenses).
func TestHandleInStreamRenewalRequestRejectsRevokedAgent(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	srv.mu.Lock()
	srv.revokedAgentIDs["agent-1"] = struct{}{}
	srv.mu.Unlock()

	sess := &fakeSendSession{}
	srv.HandleInStreamRenewalRequest(
		context.Background(), "agent-1", sess,
		&gatewayrpc.RenewalRequest{AgentId: "agent-1", CsrPem: "unused-because-revoked"},
	)

	if len(sess.sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sess.sent))
	}
	resp := sess.sent[0].GetRenewalResponse()
	if resp == nil {
		t.Fatal("response body is nil, want RenewalResponse")
	}
	if resp.GetError() == "" {
		t.Fatal("revoked agent renewal must return an error")
	}
	if resp.GetCertificatePem() != "" {
		t.Fatal("revoked agent must not receive a certificate")
	}
}

func TestHandleInStreamRenewalRequestSucceeds(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	if srv.authority == nil {
		t.Fatal("server authority is nil")
	}

	// Build a CSR for agent-1 using a fresh keypair.
	agentKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "agent-1"},
	}, agentKey)
	if err != nil {
		t.Fatalf("CreateCertificateRequest: %v", err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	sess := &fakeSendSession{}
	srv.HandleInStreamRenewalRequest(
		context.Background(), "agent-1", sess,
		&gatewayrpc.RenewalRequest{AgentId: "agent-1", CsrPem: csrPEM},
	)

	if len(sess.sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sess.sent))
	}
	resp := sess.sent[0].GetRenewalResponse()
	if resp == nil {
		t.Fatal("response body is nil, want RenewalResponse")
	}
	if resp.GetError() != "" {
		t.Fatalf("RenewalResponse.error = %q, want empty", resp.GetError())
	}
	if resp.GetCertificatePem() == "" {
		t.Fatal("RenewalResponse.certificate_pem is empty")
	}
	if resp.GetCaPem() == "" {
		t.Fatal("RenewalResponse.ca_pem is empty")
	}
	if resp.GetExpiresAtUnix() == 0 {
		t.Fatal("RenewalResponse.expires_at_unix is zero")
	}

	// Validate the returned cert chains to the panel CA.
	caBlock, _ := pem.Decode([]byte(resp.GetCaPem()))
	if caBlock == nil {
		t.Fatal("ca_pem decode failed")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate(ca): %v", err)
	}
	certBlock, _ := pem.Decode([]byte(resp.GetCertificatePem()))
	if certBlock == nil {
		t.Fatal("certificate_pem decode failed")
	}
	leafCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate(leaf): %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := leafCert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("cert verification failed: %v", err)
	}
}

func TestHandleInStreamRenewalRequestRejectsAgentIDMismatch(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	sess := &fakeSendSession{}
	srv.HandleInStreamRenewalRequest(
		context.Background(), "agent-1", sess,
		&gatewayrpc.RenewalRequest{AgentId: "agent-2", CsrPem: "irrelevant"},
	)

	if len(sess.sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sess.sent))
	}
	resp := sess.sent[0].GetRenewalResponse()
	if resp == nil {
		t.Fatal("expected RenewalResponse, got nil")
	}
	if resp.GetError() == "" {
		t.Fatal("expected error in RenewalResponse for agent_id mismatch, got empty")
	}
}

func TestHandleInStreamRenewalRequestRejectsInvalidCSR(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	sess := &fakeSendSession{}
	srv.HandleInStreamRenewalRequest(
		context.Background(), "agent-1", sess,
		&gatewayrpc.RenewalRequest{AgentId: "agent-1", CsrPem: "not-a-csr"},
	)

	if len(sess.sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sess.sent))
	}
	resp := sess.sent[0].GetRenewalResponse()
	if resp == nil {
		t.Fatal("expected RenewalResponse, got nil")
	}
	if resp.GetError() == "" {
		t.Fatal("expected error in RenewalResponse for invalid CSR, got empty")
	}
}
