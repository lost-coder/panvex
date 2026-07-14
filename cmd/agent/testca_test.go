package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testCA is the local signing CA used by cmd/agent's tests. The agent's
// credential protocols now live in internal/agent/creds and internal/agent/conn,
// each of which carries its own copy of this fixture: a test CA is scaffolding,
// not production API, so it is duplicated per package rather than exported.

type testCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
	// SPKIPin is the base64url-encoded SHA-256 of the CA's RawSubjectPublicKeyInfo.
	SPKIPin string
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	spkiHash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	pin := base64.RawURLEncoding.EncodeToString(spkiHash[:])

	return &testCA{
		cert:    cert,
		key:     priv,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		SPKIPin: pin,
	}
}

// signCSRForTest signs csrPEM with the testCA and returns a cert PEM.
func (ca *testCA) signCSRForTest(t *testing.T, csrPEM string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil {
		t.Fatal("signCSRForTest: invalid PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("signCSRForTest: ParseCertificateRequest: %v", err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatalf("signCSRForTest: CheckSignature: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("signCSRForTest: random serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, csr.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("signCSRForTest: CreateCertificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
