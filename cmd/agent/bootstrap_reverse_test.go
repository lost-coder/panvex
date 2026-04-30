package main

import (
	"context"
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
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// ---- CA / cert helpers -------------------------------------------------------

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
// The returned tls.Certificate includes the CA cert in the chain so the agent's
// VerifyPeerCertificate callback can find and pin-check it.
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

	// Include the CA cert in the chain so the agent verifier can check the pin.
	chainPEM := append(certPEM, ca.certPEM...)
	tlsCert, err := tls.X509KeyPair(chainPEM, keyPEM)
	require.NoError(t, err)
	return tlsCert
}

// freeTCPAddrT returns a free loopback TCP address; helper for tests.
func freeTCPAddrT(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

// ---- panel enroll stub -------------------------------------------------------

// panelEnrollStub dials the agent's listener, performs EnrollOutbound as the
// panel side, signs the CSR with panelCA (or the overrideCA if given), and
// sends back an EnrollCertificate. It returns any error encountered.
func panelEnrollStub(
	t *testing.T,
	listenAddr string,
	clientCert tls.Certificate,
	signingCA *testCA, // CA used to sign the agent CSR
) error {
	t.Helper()

	tlsCfg := &tls.Config{
		InsecureSkipVerify: true, // panel does not verify agent's self-signed cert
		Certificates:       []tls.Certificate{clientCert},
	}

	conn, err := grpc.NewClient(
		listenAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
	)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := gatewayrpc.NewAgentGatewayClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.EnrollOutbound(ctx)
	if err != nil {
		return err
	}

	// Recv Opening from agent (server speaks first).
	serverMsg, err := stream.Recv()
	if err != nil {
		return err
	}
	opening := serverMsg.GetOpening()
	if opening == nil {
		return errorf("expected Opening, got nil")
	}

	// Parse CSR and sign with signingCA.
	csrDER, _ := pem.Decode([]byte(opening.GetCsrPem()))
	if csrDER == nil {
		return errorf("could not decode CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(csrDER.Bytes)
	if err != nil {
		return err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	certTmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      csr.Subject,
		DNSNames:     csr.DNSNames,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, certTmpl, signingCA.cert, csr.PublicKey, signingCA.key)
	if err != nil {
		return err
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))

	// Send EnrollCertificate.
	err = stream.Send(&gatewayrpc.EnrollClientMessage{
		Body: &gatewayrpc.EnrollClientMessage_Certificate{
			Certificate: &gatewayrpc.EnrollCertificate{
				CertificatePem: certPEM,
				CaPem:          string(signingCA.certPEM),
				ExpiresAtUnix:  time.Now().Add(time.Hour).Unix(),
			},
		},
	})
	if err != nil {
		return err
	}

	return stream.CloseSend()
}

// errorf wraps a string error (avoids importing fmt/errors just for tests).
type simpleErr string

func (e simpleErr) Error() string { return string(e) }

func errorf(s string) error { return simpleErr(s) }

// ---- tests -------------------------------------------------------------------

// TestReverseBootstrapEndToEnd verifies the happy path: agent listens, panel
// dials with a valid client cert from the pinned CA, exchange completes, and
// the state file is written with TransportMode="listen".
func TestReverseBootstrapEndToEnd(t *testing.T) {
	ca := newTestCA(t)
	clientCert := ca.issueClientCert(t, "panel.local")

	listenAddr := freeTCPAddrT(t)
	stateFile := filepath.Join(t.TempDir(), "agent-state.json")

	bootstrapErrCh := make(chan error, 1)
	go func() {
		bootstrapErrCh <- reverseBootstrap(reverseBootstrapConfig{
			StateFile:      stateFile,
			BootstrapToken: "tok-abc",
			AgentID:        "agent-001",
			ListenAddr:     listenAddr,
			CAPin:          ca.SPKIPin,
			PanelCN:        "panel.local",
		})
	}()

	// Give the agent a moment to bind, then dial.
	waitForListener(t, listenAddr, 2*time.Second)

	stubErr := panelEnrollStub(t, listenAddr, clientCert, ca)
	require.NoError(t, stubErr, "panel stub error")

	bootstrapErr := <-bootstrapErrCh
	require.NoError(t, bootstrapErr, "reverseBootstrap error")

	// Verify state file.
	raw, err := os.ReadFile(stateFile)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"transport_mode": "listen"`)
	require.Contains(t, string(raw), `"agent_id": "agent-001"`)
	require.Contains(t, string(raw), `"certificate_pem"`)
	require.Contains(t, string(raw), `"private_key_pem"`)
	require.Contains(t, string(raw), `"ca_pem"`)
}

// TestReverseBootstrapRejectsWrongCAPin verifies that a panel presenting a cert
// from a different CA fails the TLS handshake and bootstrap returns an error.
func TestReverseBootstrapRejectsWrongCAPin(t *testing.T) {
	trustedCA := newTestCA(t)
	wrongCA := newTestCA(t)
	clientCert := wrongCA.issueClientCert(t, "panel.local")

	listenAddr := freeTCPAddrT(t)
	stateFile := filepath.Join(t.TempDir(), "agent-state.json")

	bootstrapErrCh := make(chan error, 1)
	go func() {
		bootstrapErrCh <- reverseBootstrap(reverseBootstrapConfig{
			StateFile:      stateFile,
			BootstrapToken: "tok-abc",
			AgentID:        "agent-002",
			ListenAddr:     listenAddr,
			CAPin:          trustedCA.SPKIPin, // trustedCA != wrongCA
			PanelCN:        "panel.local",
		})
	}()

	waitForListener(t, listenAddr, 2*time.Second)

	// Panel dials with wrong CA cert — TLS handshake should fail on agent side.
	_ = panelEnrollStub(t, listenAddr, clientCert, wrongCA) // may return err; that's fine

	// The agent should not complete enrollment; eventually the timeout or error
	// path is taken. We give it a short deadline to fail.
	select {
	case err := <-bootstrapErrCh:
		// May or may not return by now; if it did, it should not have written the file.
		if err == nil {
			t.Fatal("expected error but reverseBootstrap succeeded with wrong CA pin")
		}
		_, statErr := os.Stat(stateFile)
		require.True(t, os.IsNotExist(statErr), "state file should not exist on failure")
	case <-time.After(3 * time.Second):
		// Agent is still waiting (5-min timeout). That's acceptable: TLS
		// rejected the connection so no enrollment happened. The test just
		// verifies no state was written.
		_, statErr := os.Stat(stateFile)
		require.True(t, os.IsNotExist(statErr), "state file should not exist when no valid enrollment happened")
	}
}

// TestReverseBootstrapRejectsWrongPanelCN verifies that a panel cert with the
// correct CA but wrong CN is rejected by the agent's verifier.
func TestReverseBootstrapRejectsWrongPanelCN(t *testing.T) {
	ca := newTestCA(t)
	// Cert signed by the trusted CA but with the wrong CN.
	clientCert := ca.issueClientCert(t, "evil.attacker.example")

	listenAddr := freeTCPAddrT(t)
	stateFile := filepath.Join(t.TempDir(), "agent-state.json")

	bootstrapErrCh := make(chan error, 1)
	go func() {
		bootstrapErrCh <- reverseBootstrap(reverseBootstrapConfig{
			StateFile:      stateFile,
			BootstrapToken: "tok-abc",
			AgentID:        "agent-003",
			ListenAddr:     listenAddr,
			CAPin:          ca.SPKIPin,
			PanelCN:        "panel.local", // expected CN, but cert has "evil.attacker.example"
		})
	}()

	waitForListener(t, listenAddr, 2*time.Second)

	_ = panelEnrollStub(t, listenAddr, clientCert, ca) // handshake will fail

	select {
	case err := <-bootstrapErrCh:
		if err == nil {
			t.Fatal("expected error but reverseBootstrap succeeded with wrong panel CN")
		}
		_, statErr := os.Stat(stateFile)
		require.True(t, os.IsNotExist(statErr), "state file should not exist on failure")
	case <-time.After(3 * time.Second):
		_, statErr := os.Stat(stateFile)
		require.True(t, os.IsNotExist(statErr), "state file should not exist when panel CN mismatch")
	}
}

// waitForListener polls until the TCP address is reachable or the deadline passes.
func waitForListener(t *testing.T, addr string, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("listener at %s did not become reachable within %s", addr, deadline)
}

// TestBootstrapVerifierRejectsAttackerLeafWithLegitCAInChain reproduces the
// attack scenario from the security review: an attacker holding the panel's
// public CA generates their own keypair, builds a self-signed leaf with
// CN=panelCN, and presents the chain as [attacker_leaf, legit_CA]. The legit
// CA's SPKI matches the pin (it IS the legit CA), but the leaf isn't signed
// by it. The verifier MUST reject — otherwise enrollment hands the
// attacker's CA back to the agent as a permanent trust root.
func TestBootstrapVerifierRejectsAttackerLeafWithLegitCAInChain(t *testing.T) {
	legitCA := newTestCA(t)

	// Attacker's self-signed leaf with the right CN, signed by attacker's
	// own key (NOT by legitCA).
	attackerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	attackerTmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "panel.local"},
		DNSNames:     []string{"panel.local"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	attackerLeafDER, err := x509.CreateCertificate(rand.Reader, attackerTmpl, attackerTmpl, &attackerKey.PublicKey, attackerKey)
	require.NoError(t, err)

	verify, err := makeBootstrapVerifier(legitCA.SPKIPin, "panel.local")
	require.NoError(t, err)

	// Chain on the wire: [attacker_leaf, legit_CA].
	rawChain := [][]byte{attackerLeafDER, legitCA.cert.Raw}
	err = verify(rawChain, nil)
	require.Error(t, err, "verifier must reject leaf not chained to pinned CA")
	require.Contains(t, err.Error(), "leaf does not chain")
}

// TestBootstrapVerifierAcceptsLegitChain confirms the happy path still works
// after the chain-verification tightening: an actual leaf signed by the
// pinned CA passes.
func TestBootstrapVerifierAcceptsLegitChain(t *testing.T) {
	ca := newTestCA(t)
	leaf := ca.issueClientCert(t, "panel.local")
	require.GreaterOrEqual(t, len(leaf.Certificate), 1)

	verify, err := makeBootstrapVerifier(ca.SPKIPin, "panel.local")
	require.NoError(t, err)

	require.NoError(t, verify(leaf.Certificate, nil))
}
