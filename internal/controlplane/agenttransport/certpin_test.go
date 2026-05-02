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
	if err := verifyCertPin(cert, expected[:]); err != nil {
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
	err := verifyCertPin(cert, bogus)
	if !errors.Is(err, ErrCertPinMismatch) {
		t.Fatalf("err = %v, want ErrCertPinMismatch", err)
	}
}

func TestVerifyAgentCertPin_EmptyPinSkips(t *testing.T) {
	ca, caKey := mustGenerateCA(t)
	cert, _ := mustGenerateLeaf(t, ca, caKey, "localhost")

	if err := verifyCertPin(cert, nil); err != nil {
		t.Fatalf("verifyCertPin(nil pin): %v", err)
	}
	if err := verifyCertPin(cert, []byte{}); err != nil {
		t.Fatalf("verifyCertPin(empty pin): %v", err)
	}
}

func TestVerifyAgentCertPin_NilCert(t *testing.T) {
	bogus := make([]byte, sha256.Size)
	if _, err := rand.Read(bogus); err != nil {
		t.Fatalf("rand: %v", err)
	}
	if err := verifyCertPin(nil, bogus); !errors.Is(err, ErrCertPinMismatch) {
		t.Fatalf("err = %v, want ErrCertPinMismatch", err)
	}
}
