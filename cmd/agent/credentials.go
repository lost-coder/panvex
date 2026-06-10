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

	response, err := renewer.RenewCertificate(ctx, &gatewayrpc.RenewCertificateRequest{
		AgentId: current.AgentID,
	})
	if err != nil {
		return current, err
	}

	updated := current
	updated.CertificatePEM = response.GetCertificatePem()
	updated.PrivateKeyPEM = response.GetPrivateKeyPem()
	updated.CAPEM = response.GetCaPem()
	if response.GetExpiresAtUnix() > 0 {
		updated.ExpiresAt = time.Unix(response.GetExpiresAtUnix(), 0).UTC()
	} else {
		updated.ExpiresAt = time.Time{}
	}

	if err := agentstate.Save(stateFile, updated); err != nil {
		return current, err
	}

	return updated, nil
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
	if _, err := tls.X509KeyPair([]byte(resp.GetCertificatePem()), []byte(newKeyPEM)); err != nil {
		return current, fmt.Errorf("in-stream renewal: cert/key mismatch: %w", err)
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

	if err := agentstate.Save(stateFile, updated); err != nil {
		return current, fmt.Errorf("in-stream renewal: persist credentials: %w", err)
	}

	slog.Info("in-stream certificate renewal completed", "agent_id", current.AgentID, "expires_at", updated.ExpiresAt)
	return updated, nil
}
