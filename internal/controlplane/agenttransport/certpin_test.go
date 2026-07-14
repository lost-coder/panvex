package agenttransport

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"testing"
)

func TestVerifyAgentCertPin_Match(t *testing.T) {
	ca, caKey := mustGenerateCA(t)
	cert, _ := mustGenerateLeaf(t, ca, caKey, "localhost")

	expected := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	if err := verifyCertPin(cert, [][]byte{expected[:]}); err != nil {
		t.Fatalf("verifyCertPin(matching pin): %v", err)
	}
}

func TestVerifyAgentCertPin_Mismatch(t *testing.T) {
	ca, caKey := mustGenerateCA(t)
	cert, _ := mustGenerateLeaf(t, ca, caKey, "localhost")

	bogus := make([]byte, sha256.Size)
	if _, err := rand.Read(bogus); err != nil {
		t.Fatalf("rand: %v", err)
	}
	err := verifyCertPin(cert, [][]byte{bogus})
	if !errors.Is(err, ErrCertPinMismatch) {
		t.Fatalf("err = %v, want ErrCertPinMismatch", err)
	}
}

// R11: verifyCertPin now takes the SET of accepted pins (current plus, during a
// rotation overlap, the previous one). An empty set is a mismatch — the caller
// already fails closed on it with ErrCertPinMissing, so no dial ever reaches
// here without a candidate.
func TestVerifyAgentCertPin_EmptySetRejects(t *testing.T) {
	ca, caKey := mustGenerateCA(t)
	cert, _ := mustGenerateLeaf(t, ca, caKey, "localhost")

	if err := verifyCertPin(cert, nil); !errors.Is(err, ErrCertPinMismatch) {
		t.Fatalf("verifyCertPin(no candidates) = %v, want ErrCertPinMismatch", err)
	}
	if err := verifyCertPin(cert, [][]byte{{}}); !errors.Is(err, ErrCertPinMismatch) {
		t.Fatalf("verifyCertPin(one empty candidate) = %v, want ErrCertPinMismatch", err)
	}
}

// TestVerifyAgentCertPin_MatchesPreviousDuringOverlap is the property the whole
// overlap window exists for: a node that still holds the certificate the panel
// just replaced must still be accepted.
func TestVerifyAgentCertPin_MatchesPreviousDuringOverlap(t *testing.T) {
	ca, caKey := mustGenerateCA(t)
	cert, _ := mustGenerateLeaf(t, ca, caKey, "localhost")

	newPin := make([]byte, sha256.Size)
	if _, err := rand.Read(newPin); err != nil {
		t.Fatalf("rand: %v", err)
	}
	previous := sha256.Sum256(cert.RawSubjectPublicKeyInfo)

	if err := verifyCertPin(cert, [][]byte{newPin, previous[:]}); err != nil {
		t.Fatalf("verifyCertPin(current + previous) = %v, want accepted", err)
	}
}

func TestVerifyAgentCertPin_NilCert(t *testing.T) {
	bogus := make([]byte, sha256.Size)
	if _, err := rand.Read(bogus); err != nil {
		t.Fatalf("rand: %v", err)
	}
	if err := verifyCertPin(nil, [][]byte{bogus}); !errors.Is(err, ErrCertPinMismatch) {
		t.Fatalf("err = %v, want ErrCertPinMismatch", err)
	}
}
