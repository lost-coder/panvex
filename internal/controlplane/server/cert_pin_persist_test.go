package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// pinRecordingStore wraps a real Store and intercepts UpdateAgentCertPin
// calls so the test can assert on the persisted pin. All other methods
// delegate to the underlying real store so background workers (update
// checker, batch writer, etc.) do not hit a nil-embed panic.
type pinRecordingStore struct {
	storage.Store
	pins map[string][]byte
}

func (s *pinRecordingStore) UpdateAgentCertPin(_ context.Context, agentID string, pin []byte) error {
	s.pins[agentID] = append([]byte(nil), pin...)
	return nil
}

func TestPersistAgentCertPinStoresSPKIHash(t *testing.T) {
	now := time.Now()
	srv := testServerWithSQLite(t, now)
	store := &pinRecordingStore{Store: srv.store, pins: map[string][]byte{}}
	srv.store = store

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "agent-pin"},
	}, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	issued, err := srv.authority.issueAgentCertificateFromCSR(csrPEM, "agent-pin", agentCertificateLifetime, true, now)
	if err != nil {
		t.Fatalf("issue cert: %v", err)
	}
	srv.persistAgentCertPin(context.Background(), "agent-pin", issued.CertificatePEM)

	block, _ := pem.Decode([]byte(issued.CertificatePEM))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	got, ok := store.pins["agent-pin"]
	if !ok {
		t.Fatal("UpdateAgentCertPin was not called")
	}
	if string(got) != string(want[:]) {
		t.Fatal("persisted pin is not the SHA-256 of the cert SPKI")
	}
}
