package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// quietTelemt: a telemt fake whose runtime snapshot succeeds, so
// sendInitialMessages completes and runConnection reaches its main loop.
type quietTelemt struct{ failingTelemt }

func (quietTelemt) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	return telemt.RuntimeState{}, nil
}

// issuePEMCert issues a leaf signed by ca with the given CN/EKU and
// returns PEM strings suitable for agentstate.Credentials.
func issuePEMCert(t *testing.T, ca *testCA, cn string, eku []x509.ExtKeyUsage) (certPEM, keyPEM string) {
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
		ExtKeyUsage:  eku,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &priv.PublicKey, ca.key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

// holdingGateway accepts the Connect stream, drains inbound messages,
// and NEVER closes the stream from the server side — the whole point of
// the test is that teardown must complete without the server's help.
type holdingGateway struct {
	gatewayrpc.UnimplementedAgentGatewayServer
	connectedOnce sync.Once
	connected     chan struct{}
	release       chan struct{}
}

func (g *holdingGateway) Connect(stream gatewayrpc.AgentGateway_ConnectServer) error {
	g.connectedOnce.Do(func() { close(g.connected) })
	go func() {
		for {
			if _, err := stream.Recv(); err != nil {
				return
			}
		}
	}()
	<-g.release
	return nil
}

func TestAgentSideTeardownClosesConnectionWithoutServer(t *testing.T) {
	ca := newTestCA(t)

	// --- stub panel gateway over mTLS ---
	serverCertPEM, serverKeyPEM := issuePEMCert(t, ca, "localhost",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	serverCert, err := tls.X509KeyPair([]byte(serverCertPEM), []byte(serverKeyPEM))
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(ca.certPEM))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gw := &holdingGateway{connected: make(chan struct{}), release: make(chan struct{})}
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	})))
	gatewayrpc.RegisterAgentGatewayServer(srv, gw)
	go srv.Serve(lis) //nolint:errcheck
	t.Cleanup(func() {
		close(gw.release)
		srv.Stop()
	})

	// --- agent credentials + state file ---
	agentCertPEM, agentKeyPEM := issuePEMCert(t, ca, "agent-1",
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	creds := agentstate.Credentials{
		AgentID:        "agent-1",
		CertificatePEM: agentCertPEM,
		PrivateKeyPEM:  agentKeyPEM,
		CAPEM:          string(ca.certPEM),
		ExpiresAt:      time.Now().Add(time.Hour),
	}
	stateFile := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, agentstate.Save(stateFile, creds))

	// tr.cancel deliberately starts nil so the test can detect the moment
	// runConnection registers the real per-connection cancel.
	tr := &transportReloadState{}
	agent := runtime.New(runtime.Config{AgentID: "agent-1", NodeName: "n1", Version: "test"}, quietTelemt{})

	done := make(chan error, 1)
	go func() {
		_, err := runConnection(context.Background(), runConnectionParams{
			gatewayAddr:      lis.Addr().String(),
			serverName:       "localhost",
			stateFile:        stateFile,
			credentialsState: creds,
			agent:            agent,
			schedule:         newConnectionSchedule(0, 0, 0, 0, 0, 0),
			tr:               tr,
			reporter:         newEnrollmentReporter(),
			jobInflight:      newJobInflightTracker(),
		})
		done <- err
	}()

	select {
	case <-gw.connected:
	case <-time.After(5 * time.Second):
		t.Fatal("agent never connected to the stub gateway")
	}

	// Wait for runConnection to register the per-connection cancel.
	var cancel func()
	deadline := time.Now().Add(5 * time.Second)
	for {
		tr.mu.Lock()
		cancel = tr.cancel
		tr.mu.Unlock()
		if cancel != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tr.cancel was never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Agent-side teardown (what a switch_transport_mode job does). The
	// server holds the stream open forever, so before the fix this hangs
	// in streamWG.Wait() on the inbound pump's stream.Recv().
	start := time.Now()
	cancel()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("teardown took %v, want < 1s", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runConnection did not return after agent-side cancel (stream.Recv still blocked)")
	}
}
