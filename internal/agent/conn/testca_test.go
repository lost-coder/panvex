package conn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testCA is the local signing CA used by the connection-layer tests. Each agent
// package carries its own copy of this fixture: a test CA is scaffolding, not
// production API, so it is duplicated per package rather than exported.

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

// issueClientCert issues a client cert with the given CN, signed by this CA.
// The returned tls.Certificate includes the CA cert in the chain.
func (ca *testCA) issueClientCert(t *testing.T, cn string) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &priv.PublicKey, ca.key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	chainPEM := make([]byte, 0, len(certPEM)+len(ca.certPEM))
	chainPEM = append(chainPEM, certPEM...)
	chainPEM = append(chainPEM, ca.certPEM...)
	tlsCert, err := tls.X509KeyPair(chainPEM, keyPEM)
	require.NoError(t, err)
	return tlsCert
}
