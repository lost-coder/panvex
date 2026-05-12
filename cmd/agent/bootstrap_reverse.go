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
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"time"

	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const reverseBootstrapTimeout = 5 * time.Minute

type reverseBootstrapConfig struct {
	StateFile      string
	BootstrapToken string
	AgentID        string
	ListenAddr     string
	CAPin          string
	PanelCN        string
	// PanelURL is the gRPC endpoint (host:port) the agent should dial when
	// switching back to dial (inbound) transport mode. Persisted as
	// GRPCEndpoint so the agent can reconnect without re-bootstrapping.
	PanelURL string
}

type enrollResult struct {
	certPEM string
	caPEM   string
	expires time.Time
}

// reverseBootstrapServer wraps the grpc server and implements only EnrollOutbound.
type reverseBootstrapServer struct {
	gatewayrpc.UnimplementedAgentGatewayServer

	bootstrapToken string
	agentID        string
	csrPEM         string

	resultCh chan enrollResult
	errCh    chan error
}

func (s *reverseBootstrapServer) EnrollOutbound(stream gatewayrpc.AgentGateway_EnrollOutboundServer) error {
	// Server speaks first: send EnrollOpening.
	opening := &gatewayrpc.EnrollServerMessage{
		Body: &gatewayrpc.EnrollServerMessage_Opening{
			Opening: &gatewayrpc.EnrollOpening{
				BootstrapToken: s.bootstrapToken,
				AgentId:        s.agentID,
				CsrPem:         s.csrPEM,
			},
		},
	}
	if err := stream.Send(opening); err != nil {
		return fmt.Errorf("send EnrollOpening: %w", err)
	}

	// Wait for EnrollCertificate from panel.
	msg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("recv EnrollCertificate: %w", err)
	}

	cert := msg.GetCertificate()
	if cert == nil {
		return errors.New("expected EnrollCertificate message body")
	}
	if cert.GetCertificatePem() == "" || cert.GetCaPem() == "" {
		return errors.New("EnrollCertificate missing certificate_pem or ca_pem")
	}

	var expires time.Time
	if cert.GetExpiresAtUnix() != 0 {
		expires = time.Unix(cert.GetExpiresAtUnix(), 0).UTC()
	}

	select {
	case s.resultCh <- enrollResult{
		certPEM: cert.GetCertificatePem(),
		caPEM:   cert.GetCaPem(),
		expires: expires,
	}:
	default:
	}

	return nil
}

func reverseBootstrap(cfg reverseBootstrapConfig) error {
	// 1. Generate ECDSA P-256 keypair.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	// 2. Build CSR.
	csrPEM, err := buildCSR(key, cfg.AgentID)
	if err != nil {
		return fmt.Errorf("build csr: %w", err)
	}

	// 3. Build self-signed cert for TLS listener.
	selfCert, err := buildSelfSignedCert(key, cfg.AgentID)
	if err != nil {
		return fmt.Errorf("build self-signed cert: %w", err)
	}

	// 4. TLS config: require client cert, verify via CA-pin + panel-CN.
	verifier, err := makeBootstrapVerifier(cfg.CAPin, cfg.PanelCN)
	if err != nil {
		return fmt.Errorf("make verifier: %w", err)
	}
	tlsConfig := &tls.Config{
		ClientAuth:            tls.RequireAnyClientCert,
		Certificates:          []tls.Certificate{selfCert},
		VerifyPeerCertificate: verifier,
		NextProtos:            []string{"h2"},
		// G123: VerifyPeerCertificate runs only during the full
		// handshake; resumed TLS sessions reuse the prior verification
		// state. Disable session tickets so every handshake re-runs the
		// CA-pin + panel-CN check. The reverse-bootstrap listener is
		// short-lived and accepts a single connection, so the cost of
		// disabling resumption is negligible.
		SessionTicketsDisabled: true,
	}

	// Reverse-bootstrap is bounded by reverseBootstrapTimeout end-to-end; tie
	// the bind syscall to the same deadline so a SIGINT/timeout aborts a slow
	// kernel listen() too (noctx requires ListenConfig.Listen with a ctx).
	ctx, cancel := context.WithTimeout(context.Background(), reverseBootstrapTimeout)
	defer cancel()

	// Use a plain TCP listener; gRPC handles TLS via credentials so that ALPN
	// negotiation (h2) is performed correctly by the gRPC stack.
	var lc net.ListenConfig
	rawListener, err := lc.Listen(ctx, "tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.ListenAddr, err)
	}
	defer rawListener.Close()

	slog.Info("reverse bootstrap listening", slog.String("addr", rawListener.Addr().String()))

	// 5. Set up gRPC server.
	resultCh := make(chan enrollResult, 1)
	errCh := make(chan error, 1)

	srv := &reverseBootstrapServer{
		bootstrapToken: cfg.BootstrapToken,
		agentID:        cfg.AgentID,
		csrPEM:         csrPEM,
		resultCh:       resultCh,
		errCh:          errCh,
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))
	gatewayrpc.RegisterAgentGatewayServer(grpcServer, srv)

	listener := rawListener

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- grpcServer.Serve(listener)
	}()

	// 6. Wait for result, error, or timeout (ctx already bounded above).
	var result enrollResult
	select {
	case result = <-resultCh:
		// got it
	case serveErr := <-serveErrCh:
		if serveErr != nil {
			return fmt.Errorf("grpc serve: %w", serveErr)
		}
		return errors.New("grpc server stopped before enrollment completed")
	case <-ctx.Done():
		grpcServer.GracefulStop()
		return fmt.Errorf("reverse bootstrap timed out after %s", reverseBootstrapTimeout)
	}

	grpcServer.GracefulStop()

	// 7. Encode private key.
	privKeyPEM, err := encodePrivateKey(key)
	if err != nil {
		return fmt.Errorf("encode private key: %w", err)
	}

	// 8. Save credentials.
	// GRPCEndpoint is set from cfg.PanelURL so the agent can switch back to
	// dial (inbound) mode without re-bootstrapping: the switch_transport_mode
	// job handler only overwrites GRPCEndpoint when the job payload carries a
	// non-empty panel_url, so we must persist it here during reverse bootstrap.
	// GRPCServerName defaults to cfg.PanelCN — that is the CN the panel's cert
	// carries and therefore the name the TLS stack must verify against.
	grpcEndpoint := cfg.PanelURL
	grpcServerName := cfg.PanelCN
	creds := agentstate.Credentials{
		AgentID:        cfg.AgentID,
		CertificatePEM: result.certPEM,
		PrivateKeyPEM:  privKeyPEM,
		CAPEM:          result.caPEM,
		TransportMode:  "listen",
		ListenAddr:     cfg.ListenAddr,
		GRPCEndpoint:   grpcEndpoint,
		GRPCServerName: grpcServerName,
		ExpiresAt:      result.expires,
	}

	return agentstate.Save(cfg.StateFile, creds)
}

// buildCSR creates a PEM-encoded certificate signing request with CN=cn.
func buildCSR(key *ecdsa.PrivateKey, cn string) (string, error) {
	tmpl := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})), nil
}

// buildSelfSignedCert creates a tls.Certificate with a self-signed x509 cert
// suitable only for the bootstrap TLS listener.
func buildSelfSignedCert(key *ecdsa.PrivateKey, cn string) (tls.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(reverseBootstrapTimeout + time.Minute),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{cn},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEMBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEMBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEMBytes, keyPEMBytes)
}

// encodePrivateKey returns the PEM-encoded EC PRIVATE KEY for the given key.
func encodePrivateKey(key *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})), nil
}

// makeBootstrapVerifier returns a VerifyPeerCertificate callback that:
//   - Parses the raw DER cert chain (leaf first).
//   - Finds a cert in the chain whose SPKI SHA-256 matches caPinB64.
//   - Verifies that the leaf chains to that pinned cert as the only trusted
//     root — without this, an attacker holding the panel's public CA could
//     present a self-signed leaf with the right CN plus the legit CA appended,
//     and the pin loop would match the appended CA without ever validating
//     that the leaf was actually signed by it.
//   - Checks that the leaf's CN or one of its DNSNames matches panelCN.
func makeBootstrapVerifier(caPinB64, panelCN string) (func([][]byte, [][]*x509.Certificate) error, error) {
	pinBytes, err := base64.RawURLEncoding.DecodeString(caPinB64)
	if err != nil {
		return nil, fmt.Errorf("decode ca-pin: %w", err)
	}
	if len(pinBytes) != sha256.Size {
		return nil, fmt.Errorf("ca-pin must be %d bytes (SHA-256), got %d", sha256.Size, len(pinBytes))
	}

	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errors.New("peer presented no certificates")
		}

		certs := make([]*x509.Certificate, 0, len(rawCerts))
		for i, raw := range rawCerts {
			cert, err := x509.ParseCertificate(raw)
			if err != nil {
				return fmt.Errorf("parse peer cert[%d]: %w", i, err)
			}
			certs = append(certs, cert)
		}

		// Find a cert in the presented chain whose SPKI matches the pin.
		var pinned *x509.Certificate
		for _, c := range certs {
			spkiHash := sha256.Sum256(c.RawSubjectPublicKeyInfo)
			if string(spkiHash[:]) == string(pinBytes) {
				pinned = c
				break
			}
		}
		if pinned == nil {
			return errors.New("peer CA pin mismatch")
		}

		leaf := certs[0]

		// Verify the leaf chains to the pinned cert AND has the ClientAuth
		// EKU. We always run Verify (no leaf == pinned fast-path) so the
		// EKU check is uniformly enforced even when the panel presents a
		// self-signed cert as the leaf.
		roots := x509.NewCertPool()
		roots.AddCert(pinned)
		intermediates := x509.NewCertPool()
		for _, c := range certs[1:] {
			if c != pinned {
				intermediates.AddCert(c)
			}
		}
		if _, err := leaf.Verify(x509.VerifyOptions{
			Roots:         roots,
			Intermediates: intermediates,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}); err != nil {
			return fmt.Errorf("leaf does not chain to pinned CA: %w", err)
		}

		// Check leaf CN or SAN.
		if leaf.Subject.CommonName == panelCN {
			return nil
		}
		for _, san := range leaf.DNSNames {
			if san == panelCN {
				return nil
			}
		}
		return fmt.Errorf("peer cert CN/SAN does not match expected panel-cn %q (got CN=%q, SANs=%v)",
			panelCN, leaf.Subject.CommonName, leaf.DNSNames)
	}, nil
}

