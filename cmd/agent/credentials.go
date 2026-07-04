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
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"net/http"

	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	agentTransport "github.com/lost-coder/panvex/internal/agent/transport"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
)

type certificateRenewer interface {
	RenewCertificate(context.Context, *gatewayrpc.RenewCertificateRequest, ...grpc.CallOption) (*gatewayrpc.RenewCertificateResponse, error)
}

func loadRuntimeCredentials(stateFile string) (agentstate.Credentials, error) {
	credentialsState, err := agentstate.Load(stateFile)
	if err == nil {
		return credentialsState, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return agentstate.Credentials{}, fmt.Errorf("agent state file %q not found: bootstrap the agent first", stateFile)
	}
	return agentstate.Credentials{}, err
}

func renewRuntimeCredentialsIfNeeded(ctx context.Context, stateFile string, gatewayAddr string, serverName string, current agentstate.Credentials, now time.Time) (agentstate.Credentials, error) {
	if !runtimeCredentialsNeedRefresh(current, now) {
		return current, nil
	}
	if runtimeCredentialsNeedRecovery(current, now) {
		return recoverRuntimeCredentialsIfNeeded(ctx, stateFile, current, nil, now)
	}

	certificate, err := tls.X509KeyPair([]byte(current.CertificatePEM), []byte(current.PrivateKeyPEM))
	if err != nil {
		return current, err
	}

	cfg := agentTransport.DialConfig{
		GatewayAddr: gatewayAddr,
		ServerName:  serverName,
		CAPEM:       current.CAPEM,
		Cert:        certificate,
	}

	var updated agentstate.Credentials
	runErr := agentTransport.NewDialTransport(cfg).RunOnce(ctx, func(_ context.Context, _ agentTransport.BidiStream, client gatewayrpc.AgentGatewayClient) error {
		var refreshErr error
		updated, refreshErr = refreshRuntimeCredentialsIfNeeded(ctx, stateFile, current, client, now)
		return refreshErr
	})
	if runErr != nil {
		return current, runErr
	}
	return updated, nil
}

func refreshRuntimeCredentialsIfNeeded(ctx context.Context, stateFile string, current agentstate.Credentials, renewer certificateRenewer, now time.Time) (agentstate.Credentials, error) {
	if !runtimeCredentialsNeedRefresh(current, now) {
		return current, nil
	}

	newKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return current, fmt.Errorf("unary renewal: generate key: %w", err)
	}
	csrPEM, err := buildCSRPEM(current.AgentID, newKey)
	if err != nil {
		return current, fmt.Errorf("unary renewal: build CSR: %w", err)
	}

	response, err := renewer.RenewCertificate(ctx, &gatewayrpc.RenewCertificateRequest{
		AgentId: current.AgentID,
		CsrPem:  csrPEM,
	})
	if err != nil {
		return current, err
	}

	newKeyDER, err := x509.MarshalECPrivateKey(newKey)
	if err != nil {
		return current, fmt.Errorf("unary renewal: marshal key: %w", err)
	}
	newKeyPEM := encodeCertPEM("EC PRIVATE KEY", newKeyDER)
	newCert, err := tls.X509KeyPair([]byte(response.GetCertificatePem()), []byte(newKeyPEM))
	if err != nil {
		return current, fmt.Errorf("unary renewal: cert/key mismatch: %w", err)
	}
	if err := checkRenewedCertCN(newCert, current.AgentID); err != nil {
		slog.ErrorContext(ctx, "unary renewal: issued certificate CN does not match agent identity; refusing to apply",
			"agent_id", current.AgentID, "issued_cn", renewedCertCN(newCert), "error", err)
		return current, fmt.Errorf("unary renewal: %w", err)
	}

	updated := current
	updated.CertificatePEM = response.GetCertificatePem()
	updated.PrivateKeyPEM = newKeyPEM
	updated.CAPEM = response.GetCaPem()
	if response.GetExpiresAtUnix() > 0 {
		updated.ExpiresAt = time.Unix(response.GetExpiresAtUnix(), 0).UTC()
	} else {
		updated.ExpiresAt = time.Time{}
	}

	// Audit #7: patch the cert fields onto the FRESH on-disk state under
	// the package write lock — a concurrent usage-seq tick between our
	// Load and Save must not be lost, and vice versa.
	persisted, err := agentstate.Update(stateFile, func(c *agentstate.Credentials) {
		c.CertificatePEM = updated.CertificatePEM
		c.PrivateKeyPEM = updated.PrivateKeyPEM
		c.CAPEM = updated.CAPEM
		c.ExpiresAt = updated.ExpiresAt
	})
	if err != nil {
		return current, err
	}

	return persisted, nil
}

func runtimeCredentialsNeedRefresh(current agentstate.Credentials, now time.Time) bool {
	if current.AgentID == "" {
		return false
	}
	if current.ExpiresAt.IsZero() {
		return false
	}

	return !now.Add(runtimeCertificateRenewWindow).Before(current.ExpiresAt.UTC())
}

func runtimeCredentialsNeedRecovery(current agentstate.Credentials, now time.Time) bool {
	if strings.TrimSpace(current.PanelURL) == "" {
		return false
	}
	if current.ExpiresAt.IsZero() {
		return false
	}

	return !current.ExpiresAt.UTC().After(now.UTC())
}

func runtimeCredentialRefreshDelay(current agentstate.Credentials, now time.Time) time.Duration {
	if runtimeCredentialsNeedRefresh(current, now) {
		return 0
	}

	refreshAt := current.ExpiresAt.UTC().Add(-runtimeCertificateRenewWindow)
	if !refreshAt.After(now) {
		return 0
	}

	return refreshAt.Sub(now)
}

func newRuntimeCredentialRefreshTimer(current agentstate.Credentials, now time.Time) *time.Timer {
	if current.ExpiresAt.IsZero() {
		return nil
	}

	return time.NewTimer(runtimeCredentialRefreshDelay(current, now))
}

func resetRuntimeCredentialRefreshTimer(timer *time.Timer, delay time.Duration) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

// buildCSRPEM builds a CERTIFICATE REQUEST PEM block signed by key with
// the agent's CN.
func buildCSRPEM(agentID string, key *ecdsa.PrivateKey) (string, error) {
	tmpl := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   agentID,
			Organization: []string{"Panvex Agents"},
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return "", err
	}
	return encodeCertPEM("CERTIFICATE REQUEST", csrDER), nil
}

// encodeCertPEM encodes der bytes as a PEM block with the given type.
func encodeCertPEM(blockType string, der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}))
}

// errRenewalCNMismatch is returned by checkRenewedCertCN when the leaf
// certificate returned by a renewal response does not carry the expected
// agent ID as its CommonName. Kept as a sentinel so callers/tests can assert
// on it with errors.Is rather than string-matching (3.6).
var errRenewalCNMismatch = errors.New("renewal: certificate CN mismatch")

// checkRenewedCertCN validates that a freshly-renewed certificate's leaf CN
// matches the expected agent ID before the caller persists/applies it.
// Both the unary (refreshRuntimeCredentialsIfNeeded) and in-stream
// (renewCertificateInStream) renewal paths call this immediately after their
// tls.X509KeyPair pairing check succeeds — a cert that pairs with the key we
// just generated but was issued for a DIFFERENT agent identity indicates a
// misrouted or malicious panel response and must not be adopted.
//
// cert.Leaf is populated by tls.X509KeyPair as of Go 1.23; the explicit
// re-parse of Certificate[0] is a defensive fallback in case that invariant
// ever changes upstream.
func checkRenewedCertCN(cert tls.Certificate, expectedAgentID string) error {
	leaf := cert.Leaf
	if leaf == nil {
		if len(cert.Certificate) == 0 {
			return fmt.Errorf("%w: no certificate bytes to inspect", errRenewalCNMismatch)
		}
		parsed, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return fmt.Errorf("renewal: parse leaf certificate: %w", err)
		}
		leaf = parsed
	}
	if leaf.Subject.CommonName != expectedAgentID {
		return fmt.Errorf("%w: got %q, want %q", errRenewalCNMismatch, leaf.Subject.CommonName, expectedAgentID)
	}
	return nil
}

// renewedCertCN best-effort extracts the leaf CommonName for logging
// alongside a checkRenewedCertCN failure. Returns "" if the leaf cannot be
// determined — logging must never itself panic on a malformed response.
func renewedCertCN(cert tls.Certificate) string {
	leaf := cert.Leaf
	if leaf == nil {
		if len(cert.Certificate) == 0 {
			return ""
		}
		parsed, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return ""
		}
		leaf = parsed
	}
	return leaf.Subject.CommonName
}

// recoverListenCredentialsIfExpired handles the listen-mode dead end the
// audit flagged: an EXPIRED cert cannot complete any mTLS handshake (neither
// the panel's dial-in nor in-stream renewal), and the dial-mode unary
// renewal pre-flight is skipped in listen mode. The HTTP certificate
// recovery flow works over HTTPS to PanelURL regardless of transport mode,
// so run it before re-entering the listen loop. client==nil uses the
// default bootstrap HTTP client.
func recoverListenCredentialsIfExpired(ctx context.Context, stateFile string, current agentstate.Credentials, client *http.Client, now time.Time) (agentstate.Credentials, error) {
	if !runtimeCredentialsNeedRecovery(current, now) {
		return current, nil
	}
	slog.Warn("listen mode: certificate expired; attempting HTTP recovery",
		"agent_id", current.AgentID, "expired_at", current.ExpiresAt.UTC().Format(time.RFC3339))
	return recoverRuntimeCredentialsIfNeeded(ctx, stateFile, current, client, now)
}

// renewCertificateInStream performs in-stream cert renewal over the existing
// Connect bidi-stream. It generates a fresh ECDSA P-256 keypair, builds a
// CSR signed with the new key, sends a RenewalRequest via criticalOutbound,
// and waits up to certificateRefreshTimeout for the panel's RenewalResponse.
// On success it validates the returned cert pairs with the new key, atomically
// updates the in-memory credentials, and persists them to disk.
func renewCertificateInStream(
	ctx context.Context,
	current agentstate.Credentials,
	stateFile string,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
	renewalResponses <-chan *gatewayrpc.RenewalResponse,
) (agentstate.Credentials, error) {
	// Generate fresh keypair — private key never leaves the agent.
	newKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return current, fmt.Errorf("in-stream renewal: generate key: %w", err)
	}

	csrPEM, err := buildCSRPEM(current.AgentID, newKey)
	if err != nil {
		return current, fmt.Errorf("in-stream renewal: build CSR: %w", err)
	}

	// Enqueue the renewal request — the outbound pump will send it.
	msg := &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_RenewalRequest{
			RenewalRequest: &gatewayrpc.RenewalRequest{
				AgentId: current.AgentID,
				CsrPem:  csrPEM,
			},
		},
	}
	select {
	case criticalOutbound <- msg:
	case <-ctx.Done():
		return current, ctx.Err()
	}

	// Wait for the panel's response.
	renewCtx, cancel := context.WithTimeout(ctx, certificateRefreshTimeout)
	defer cancel()
	var resp *gatewayrpc.RenewalResponse
	select {
	case resp = <-renewalResponses:
	case <-renewCtx.Done():
		return current, fmt.Errorf("in-stream renewal: timeout waiting for response")
	}

	if resp.GetError() != "" {
		return current, fmt.Errorf("in-stream renewal: panel rejected: %s", resp.GetError())
	}

	// Validate: the cert must pair with the new key we generated.
	newKeyDER, err := x509.MarshalECPrivateKey(newKey)
	if err != nil {
		return current, fmt.Errorf("in-stream renewal: marshal new key: %w", err)
	}
	newKeyPEM := encodeCertPEM("EC PRIVATE KEY", newKeyDER)
	newCert, err := tls.X509KeyPair([]byte(resp.GetCertificatePem()), []byte(newKeyPEM))
	if err != nil {
		return current, fmt.Errorf("in-stream renewal: cert/key mismatch: %w", err)
	}
	if err := checkRenewedCertCN(newCert, current.AgentID); err != nil {
		slog.ErrorContext(ctx, "in-stream renewal: issued certificate CN does not match agent identity; refusing to apply",
			"agent_id", current.AgentID, "issued_cn", renewedCertCN(newCert), "error", err)
		return current, fmt.Errorf("in-stream renewal: %w", err)
	}

	updated := current
	updated.CertificatePEM = resp.GetCertificatePem()
	updated.PrivateKeyPEM = newKeyPEM
	if resp.GetCaPem() != "" {
		updated.CAPEM = resp.GetCaPem()
	}
	if resp.GetExpiresAtUnix() > 0 {
		updated.ExpiresAt = time.Unix(resp.GetExpiresAtUnix(), 0).UTC()
	}

	// Audit #7: same rationale as the unary path — merge onto fresh disk state.
	persisted, err := agentstate.Update(stateFile, func(c *agentstate.Credentials) {
		c.CertificatePEM = updated.CertificatePEM
		c.PrivateKeyPEM = updated.PrivateKeyPEM
		c.CAPEM = updated.CAPEM
		c.ExpiresAt = updated.ExpiresAt
	})
	if err != nil {
		return current, fmt.Errorf("in-stream renewal: persist credentials: %w", err)
	}

	slog.Info("in-stream certificate renewal completed", "agent_id", current.AgentID, "expires_at", persisted.ExpiresAt)
	return persisted, nil
}
