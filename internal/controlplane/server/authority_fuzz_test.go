package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"
)

// FuzzSignCSR feeds arbitrary bytes to certificateAuthority.SignCSR, which
// parses a CSR PEM supplied by an untrusted agent during enrollment /
// renewal. The PEM decode + x509.ParseCertificateRequest + signature-check
// path must never panic on malformed input; it must return an error instead.
func FuzzSignCSR(f *testing.F) {
	ca, err := newCertificateAuthority(time.Now())
	if err != nil {
		f.Fatalf("build CA: %v", err)
	}

	// Seed: a well-formed CSR with the expected CN.
	const agentID = "agent-fuzz-0001"
	if good := makeCSR(f, agentID); good != "" {
		f.Add([]byte(good))
	}
	// Seed: a CSR whose CN will not match agentID (signs path reaches CN check).
	if mismatch := makeCSR(f, "someone-else"); mismatch != "" {
		f.Add([]byte(mismatch))
	}
	// Edge cases.
	f.Add([]byte(""))
	f.Add([]byte("-----BEGIN CERTIFICATE REQUEST-----\nnot base64\n-----END CERTIFICATE REQUEST-----"))
	f.Add([]byte("-----BEGIN CERTIFICATE REQUEST-----\n-----END CERTIFICATE REQUEST-----"))
	f.Add([]byte("garbage not a pem block at all"))

	f.Fuzz(func(t *testing.T, csrPEM []byte) {
		// Must never panic. Errors are the expected outcome for garbage.
		_, _, _, _ = ca.SignCSR(string(csrPEM), agentID, time.Hour)
	})
}

// makeCSR builds a valid CSR PEM with the given CN, or returns "" if
// generation fails (which should not happen in practice).
func makeCSR(f *testing.F, cn string) string {
	f.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return ""
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn},
	}, key)
	if err != nil {
		return ""
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}
