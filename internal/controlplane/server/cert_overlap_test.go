package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// The certificate-overlap window (R11 / R-1). Issuance and delivery are not
// atomic: the panel records the new credential, then sends the certificate. If
// that exchange is interrupted — panel restart, stream drop — the agent still
// holds the old certificate. A verifier that only knows the new one refuses it,
// and a listen-mode node has no way to ask for another signature: it sat
// stranded until an operator issued a recovery grant.
//
// These tests pin the acceptance rules from both sides of the connection.

func TestRenewalInterruptedNodeKeepsConnectingWithPreviousCert(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	currentTime := now
	server := &Server{now: func() time.Time { return currentTime }}

	overlapUntil := now.Add(24 * time.Hour)
	pins := storage.AgentCertPins{
		Serial:       "serial-new",
		SPKI:         bytes.Repeat([]byte{0x02}, 32),
		PrevSerial:   "serial-old",
		PrevSPKI:     bytes.Repeat([]byte{0x01}, 32),
		OverlapUntil: &overlapUntil,
	}

	// The node never received the renewal response, so it presents the old one.
	if got := server.classifyPresentedSerial(pins, "serial-old"); got != certSerialPrevious {
		t.Fatalf("presenting the previous serial during the overlap = %v, want certSerialPrevious", got)
	}
	// The new one is of course accepted, and it is what closes the window.
	if got := server.classifyPresentedSerial(pins, "serial-new"); got != certSerialCurrent {
		t.Fatalf("presenting the current serial = %v, want certSerialCurrent", got)
	}
	// Anything else stays refused — the overlap widens the accepted set to two
	// credentials, not to any credential.
	if got := server.classifyPresentedSerial(pins, "serial-harvested"); got != certSerialUnknown {
		t.Fatalf("presenting an unknown serial = %v, want certSerialUnknown", got)
	}
}

func TestOverlapWindowExpires(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	currentTime := now
	server := &Server{now: func() time.Time { return currentTime }}

	overlapUntil := now.Add(time.Hour)
	pins := storage.AgentCertPins{
		Serial:       "serial-new",
		SPKI:         bytes.Repeat([]byte{0x02}, 32),
		PrevSerial:   "serial-old",
		PrevSPKI:     bytes.Repeat([]byte{0x01}, 32),
		OverlapUntil: &overlapUntil,
	}

	currentTime = now.Add(2 * time.Hour)
	if got := server.classifyPresentedSerial(pins, "serial-old"); got != certSerialUnknown {
		t.Fatalf("presenting the previous serial after the window closed = %v, want certSerialUnknown", got)
	}
}

func TestClosedOverlapRefusesThePreviousCert(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	server := &Server{now: func() time.Time { return now }}

	// What CloseAgentCertOverlap leaves behind: current only.
	pins := storage.AgentCertPins{Serial: "serial-new", SPKI: bytes.Repeat([]byte{0x02}, 32)}

	if got := server.classifyPresentedSerial(pins, "serial-old"); got != certSerialUnknown {
		t.Fatalf("presenting the previous serial after the overlap closed = %v, want certSerialUnknown", got)
	}
}

// The dial path (listen-mode nodes) must see the same accepted set as the
// inbound path — that is the path where an interrupted renewal actually
// stranded a node.
func TestAcceptedAgentCertPinsIncludesPreviousDuringOverlap(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	currentTime := now
	overlapUntil := now.Add(time.Hour)

	store := &certPinsStub{pins: storage.AgentCertPins{
		Serial:       "serial-new",
		SPKI:         bytes.Repeat([]byte{0x02}, 32),
		PrevSerial:   "serial-old",
		PrevSPKI:     bytes.Repeat([]byte{0x01}, 32),
		OverlapUntil: &overlapUntil,
	}}
	server := &Server{now: func() time.Time { return currentTime }, store: store}
	reader := certPinReader{server: server}

	accepted, err := reader.AcceptedAgentCertPins(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("AcceptedAgentCertPins() error = %v", err)
	}
	if len(accepted) != 2 {
		t.Fatalf("len(accepted) = %d, want 2 (current + previous)", len(accepted))
	}

	// Once the window lapses, only the current pin is accepted.
	currentTime = now.Add(2 * time.Hour)
	accepted, err = reader.AcceptedAgentCertPins(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("AcceptedAgentCertPins() after expiry error = %v", err)
	}
	if len(accepted) != 1 || !bytes.Equal(accepted[0], store.pins.SPKI) {
		t.Fatalf("after the window closed, accepted = %d pins, want only the current one", len(accepted))
	}
}

// newOverlapTestCSR builds a CSR for agentID with a fresh keypair, the way the
// agent does for each renewal attempt.
func newOverlapTestCSR(t *testing.T, agentID string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: agentID},
	}, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))
}

// TestSecondRenewalOverPreviousCertKeepsAgentAccepted reconstructs the R11-1
// failing sequence end-to-end against a real SQLite store:
//
//	t0  the agent holds certificate A (enrollment; pins {cur=A}).
//	t1  in-stream renewal #1 over the A-authenticated stream rotates the pins
//	    to {cur=B, prev=A, window open} — and the RenewalResponse is LOST, so
//	    the agent never learns about B.
//	t2  the agent reconnects still holding A (accepted: previous, window open;
//	    a prev-cert connect does not close the window).
//	t3  its renewal timer fires again within a minute — renewal #2, also asked
//	    over A. Before the fix this shifted prev := B (a certificate nobody
//	    ever received), evicting A from the accepted set.
//	t4  response #2 is lost too. The agent's only credential is A — it must
//	    still classify as accepted, or a listen-mode node is stranded until an
//	    operator recovery grant: the exact failure R11 Task 1 closed.
func TestSecondRenewalOverPreviousCertKeepsAgentAccepted(t *testing.T) {
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	srv := testServerWithSQLite(t, now)
	ctx := context.Background()

	const agentID = "agent-r11"
	if err := srv.store.PutFleetGroup(ctx, storage.FleetGroupRecord{ID: "fg-overlap", Name: "Default", CreatedAt: now}); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}
	if err := srv.store.PutAgent(ctx, storage.AgentRecord{
		ID: agentID, NodeName: "node-r11", FleetGroupID: "fg-overlap", Version: "dev", LastSeenAt: now,
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	// t0: enrollment — certificate A, no presented credential, no window.
	issuedA, err := srv.authority.issueAgentCertificateFromCSR(newOverlapTestCSR(t, agentID), agentID, agentCertificateLifetime, true, now)
	if err != nil {
		t.Fatalf("issue certificate A: %v", err)
	}
	srv.rotateAgentCredential(ctx, agentID, issuedA.CertificatePEM, "")
	serialA := issuedA.Serial

	renew := func(label string) *gatewayrpc.RenewalResponse {
		t.Helper()
		sess := &fakeSendSession{}
		srv.HandleInStreamRenewalRequest(ctx, agentID, serialA, sess,
			&gatewayrpc.RenewalRequest{AgentId: agentID, CsrPem: newOverlapTestCSR(t, agentID)})
		if len(sess.sent) != 1 {
			t.Fatalf("%s: len(sent) = %d, want 1", label, len(sess.sent))
		}
		resp := sess.sent[0].GetRenewalResponse()
		if resp == nil || resp.GetError() != "" {
			t.Fatalf("%s: renewal failed: %+v", label, resp)
		}
		return resp
	}

	// t1: renewal #1 over the A-authenticated stream; response lost.
	renew("renewal#1")
	// t2–t3: the agent reconnects with A and asks again; response lost again.
	respC := renew("renewal#2")

	// t4: the agent's only credential A must still be accepted.
	pins, err := srv.store.GetAgentCertPins(ctx, agentID)
	if err != nil {
		t.Fatalf("GetAgentCertPins() error = %v", err)
	}
	if got := srv.classifyPresentedSerial(pins, serialA); got != certSerialPrevious {
		t.Fatalf("after two lost renewal responses, presenting the certificate the agent still holds = %v (pins cur=%q prev=%q), want certSerialPrevious",
			got, pins.Serial, pins.PrevSerial)
	}

	// The freshly issued certificate from renewal #2 is the pinned current one,
	// so delivery of THAT response converges the agent normally.
	certBlock, _ := pem.Decode([]byte(respC.GetCertificatePem()))
	if certBlock == nil {
		t.Fatal("renewal#2 certificate_pem decode failed")
	}
	leaf, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("parse renewal#2 leaf: %v", err)
	}
	if want := leaf.SerialNumber.Text(16); pins.Serial != want {
		t.Fatalf("pins.Serial = %q, want the renewal#2 serial %q", pins.Serial, want)
	}
}

// certPinsStub is a storage.Store whose only implemented method is the one the
// pin reader calls. Anything else would panic, which is exactly what we want
// from a stub: a test that grows a second dependency should say so loudly.
type certPinsStub struct {
	storage.Store
	pins storage.AgentCertPins
}

func (s *certPinsStub) GetAgentCertPins(context.Context, string) (storage.AgentCertPins, error) {
	return s.pins, nil
}
