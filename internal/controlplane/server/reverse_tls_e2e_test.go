package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"

	transport "github.com/lost-coder/panvex/internal/agent/transport"
	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

// TestReverseTransportRedialWithAuthorityIssuedCerts is the regression test
// for audit A1: the steady-state panel→agent re-dial must work with
// AUTHORITY-ISSUED certificates.  It exercises:
//   - CSR issuance via issueAgentCertificateFromCSR (the real authority path)
//   - outboundTLSConfig + per-agent ServerName from agenttransport.AgentServerName
//   - The agent listen transport (NewListenTransport) with PanelCN verification
//   - A JobCommand → JobResult round-trip over the inverted-type stream
//   - A SECOND successful dial after the first RunOnce returns (re-dial)
func TestReverseTransportRedialWithAuthorityIssuedCerts(t *testing.T) {
	now := time.Now()
	authority, err := newCertificateAuthority(now)
	if err != nil {
		t.Fatalf("newCertificateAuthority: %v", err)
	}

	// --- Generate agent key + CSR, issue cert via the real authority path ---
	const agentID = "01890000-0000-7000-8000-00000000e2e1"
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("agent key: %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: agentID},
	}, key)
	if err != nil {
		t.Fatalf("csr: %v", err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))
	issued, err := authority.issueAgentCertificateFromCSR(csrPEM, agentID, time.Hour, true, now)
	if err != nil {
		t.Fatalf("issue agent cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	agentCert, err := tls.X509KeyPair([]byte(issued.CertificatePEM), keyPEM)
	if err != nil {
		t.Fatalf("agent keypair: %v", err)
	}

	// Compute SPKI pin for the agent's leaf cert (used to verify the panel
	// can see the right peer cert on dial).
	leafBlock, _ := pem.Decode([]byte(issued.CertificatePEM))
	leaf, err := x509.ParseCertificate(leafBlock.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	pin := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)

	// --- Build the listen transport (agent side) ---
	lt := transport.NewListenTransport(transport.ListenConfig{
		Addr:    "127.0.0.1:0",
		Cert:    agentCert,
		CAPEM:   authority.caPEM,
		PanelCN: PanelClientCN,
	})

	// runner: agent receives one ConnectServerMessage (job), replies with
	// one ConnectClientMessage (result), then returns — ending RunOnce.
	runner := transport.SessionRunner(func(_ context.Context, stream transport.BidiStream, _ gatewayrpc.AgentGatewayClient) error {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		job := msg.GetJob()
		if job == nil {
			t.Errorf("agent received non-job body %T", msg.GetBody())
			return nil
		}
		return stream.Send(&gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_JobResult{
				JobResult: &gatewayrpc.JobResult{
					AgentId: agentID,
					JobId:   job.GetId(),
					Success: true,
				},
			},
		})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// listenErr channels collect RunOnce returns.
	listenErr := make(chan error, 2)
	startListen := func() {
		go func() { listenErr <- lt.RunOnce(ctx, runner) }()
	}
	startListen()

	// Wait for the listener to bind (Address() is set under a mutex inside
	// RunOnce, just after lc.Listen succeeds).
	addr := waitForAddress(t, lt, 5*time.Second, "")

	// dialOnce: panel dials the agent, sends a job, receives the result.
	// The panel is the gRPC CLIENT, so its stream's native type is:
	//   Send → ConnectClientMessage, Recv → ConnectServerMessage
	// But the serverStreamAdapter inverts types on the agent side with raw
	// SendMsg/RecvMsg.  We mirror that here:
	//   panel → agent job:    SendMsg(&ConnectServerMessage{Job: ...})
	//   agent → panel result: RecvMsg(&ConnectClientMessage{})
	dialOnce := func(jobID string) {
		tlsCfg := authority.outboundTLSConfig().Clone()
		tlsCfg.ServerName = agenttransport.AgentServerName(agentID)
		// Extra guard: verify the peer's SPKI pin matches what we issued.
		tlsCfg.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				t.Errorf("%s: no peer cert in TLS state", jobID)
				return nil
			}
			got := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
			if got != pin {
				t.Errorf("%s: SPKI pin mismatch on outbound dial", jobID)
			}
			return nil
		}
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                30 * time.Second,
				Timeout:             10 * time.Second,
				PermitWithoutStream: true,
			}),
		)
		if err != nil {
			t.Fatalf("%s: grpc.NewClient: %v", jobID, err)
		}
		defer conn.Close()

		stream, err := gatewayrpc.NewAgentGatewayClient(conn).Connect(ctx)
		if err != nil {
			t.Fatalf("%s: Connect: %v", jobID, err)
		}

		// Send the job using raw SendMsg so we can send ConnectServerMessage
		// bytes over the client stream (which normally sends ConnectClientMessage).
		// The serverStreamAdapter on the agent side does the same inversion.
		if err := stream.SendMsg(&gatewayrpc.ConnectServerMessage{
			Body: &gatewayrpc.ConnectServerMessage_Job{
				Job: &gatewayrpc.JobCommand{
					Id:     jobID,
					Action: "runtime.reload",
				},
			},
		}); err != nil {
			t.Fatalf("%s: send job: %v", jobID, err)
		}

		// Receive the agent's result via raw RecvMsg as ConnectClientMessage.
		reply := &gatewayrpc.ConnectClientMessage{}
		if err := stream.RecvMsg(reply); err != nil {
			t.Fatalf("%s: recv result: %v", jobID, err)
		}
		jr := reply.GetJobResult()
		if jr == nil || jr.GetJobId() != jobID || !jr.GetSuccess() {
			t.Fatalf("%s: bad result %+v", jobID, reply)
		}
	}

	// --- First dial + job round-trip ---
	dialOnce("job-1")
	if err := <-listenErr; err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}

	// --- Re-dial: start a second RunOnce and confirm it also succeeds ---
	startListen()
	addr = waitForAddress(t, lt, 5*time.Second, addr)
	dialOnce("job-2")
	if err := <-listenErr; err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
}

// waitForAddress polls lt.Address() until it returns a non-empty value that
// differs from prev, or the deadline expires.  Pass prev="" on first call.
func waitForAddress(t *testing.T, lt transport.Transport, timeout time.Duration, prev string) string {
	t.Helper()
	type addresser interface{ Address() string }
	a, ok := lt.(addresser)
	if !ok {
		t.Fatal("listen transport does not implement Address()")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if addr := a.Address(); addr != "" && addr != prev {
			return addr
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("listen transport never bound a new address")
	return ""
}
