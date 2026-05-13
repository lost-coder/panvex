package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	agentstate "github.com/lost-coder/panvex/internal/agent/state"
)

const agentBootstrapPath = "/api/agent/bootstrap"
const agentCertificateRecoveryPath = "/api/agent/recover-certificate"
const bootstrapRequestTimeout = 15 * time.Second

type bootstrapConfig struct {
	PanelURL        string
	EnrollmentToken string
	StateFile       string
	NodeName        string
	Version         string
	Force           bool
	// InsecureTransport opts out of the "HTTPS required unless loopback"
	// guard in `agentEndpointURL`. Only meaningful for private-network or
	// VPN-only deployments where the operator has already decided the
	// link between agent and panel is trusted. See the `-insecure-transport`
	// CLI flag; the choice is persisted into the credentials state so
	// certificate recovery can honor it on subsequent runs.
	InsecureTransport bool
}

type bootstrapRequest struct {
	NodeName string `json:"node_name"`
	Version  string `json:"version"`
}

type bootstrapResponse struct {
	AgentID        string `json:"agent_id"`
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	CAPEM          string `json:"ca_pem"`
	GRPCEndpoint   string `json:"grpc_endpoint"`
	GRPCServerName string `json:"grpc_server_name"`
	ExpiresAtUnix  int64  `json:"expires_at_unix"`
	// AttemptID is the enrollment.Recorder attempt id opened on the panel
	// for this bootstrap call (Phase 1 enrollment-logging). The agent
	// persists it so the runtime can ship local steps via
	// ReportEnrollmentSteps once the first sync is up. Older panels omit
	// the field — empty string is treated as "no reporting".
	AttemptID string `json:"attempt_id"`
}

type agentCertificateRecoveryRequest struct {
	AgentID            string `json:"agent_id"`
	CertificatePEM     string `json:"certificate_pem"`
	ProofTimestampUnix int64  `json:"proof_timestamp_unix"`
	ProofNonce         string `json:"proof_nonce"`
	ProofSignature     string `json:"proof_signature"`
}

func runBootstrapCommand(args []string, client *http.Client) error {
	flags := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	panelURL := flags.String("panel-url", "", "Control-plane HTTPS base URL")
	enrollmentToken := flags.String("enrollment-token", "", "One-time enrollment token")
	stateFile := flags.String("state-file", "data/agent-state.json", "Agent credential state file")
	nodeName := flags.String("node-name", hostName(), "Node name reported to the control-plane")
	version := flags.String("version", "dev", "Agent version")
	force := flags.Bool("force", false, "Overwrite an existing state file")
	insecureTransport := flags.Bool("insecure-transport", false,
		"Allow http:// panel URLs on non-loopback hosts. Use only on trusted private networks (e.g. VPN-only links) — bootstrap exchanges the private key in cleartext when this is set.")
	mode := flags.String("mode", "dial", "bootstrap mode: dial | reverse")
	bootstrapToken := flags.String("bootstrap-token", "", "raw bootstrap token (reverse mode)")
	agentID := flags.String("agent-id", "", "agent identifier (reverse mode)")
	listenAddr := flags.String("listen-addr", ":8443", "TCP listen address (reverse mode)")
	caPin := flags.String("ca-pin", "", "SHA-256 SPKI hash of panel CA, base64url (reverse mode)")
	panelCN := flags.String("panel-cn", "", "expected CN/SAN of panel client cert (reverse mode)")
	reversePanelURL := flags.String("panel-url-grpc", "", "gRPC endpoint of the panel, host:port (reverse mode, e.g. panel.example.com:8443)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *stateFile == "" {
		return errors.New("bootstrap requires -state-file")
	}

	if *mode == "reverse" {
		if *bootstrapToken == "" || *agentID == "" || *caPin == "" || *panelCN == "" {
			return errors.New("reverse bootstrap requires --bootstrap-token, --agent-id, --ca-pin, --panel-cn")
		}
		if *reversePanelURL == "" {
			return errors.New("reverse bootstrap requires --panel-url-grpc (gRPC endpoint, e.g. panel.example.com:8443)")
		}
		return reverseBootstrap(reverseBootstrapConfig{
			StateFile:      *stateFile,
			BootstrapToken: *bootstrapToken,
			AgentID:        *agentID,
			ListenAddr:     *listenAddr,
			CAPin:          *caPin,
			PanelCN:        *panelCN,
			PanelURL:       *reversePanelURL,
		})
	}

	if *panelURL == "" || *enrollmentToken == "" {
		return errors.New("bootstrap requires -panel-url, -enrollment-token, and -state-file")
	}

	if !*force {
		if _, err := os.Stat(*stateFile); err == nil {
			return errors.New("bootstrap requires -force when the state file already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if *insecureTransport {
		// Loud warning on every bootstrap. Operators who flipped the flag
		// knowingly will ignore it; anyone who flipped it by accident will
		// see the drift in their install logs and back it out.
		slog.Warn("bootstrap over insecure transport",
			slog.String("panel_url", *panelURL),
			slog.String("hint", "private key and certificate will transit unencrypted; only use on VPN / private-network links"))
	}

	credentialsState, err := bootstrapAgent(context.Background(), client, bootstrapConfig{
		PanelURL:          *panelURL,
		EnrollmentToken:   *enrollmentToken,
		StateFile:         *stateFile,
		NodeName:          *nodeName,
		Version:           *version,
		Force:             *force,
		InsecureTransport: *insecureTransport,
	})
	if err != nil {
		return err
	}

	return agentstate.Save(*stateFile, credentialsState)
}

func bootstrapAgent(ctx context.Context, client *http.Client, config bootstrapConfig) (agentstate.Credentials, error) {
	endpoint, err := bootstrapEndpointURL(config.PanelURL, config.InsecureTransport)
	if err != nil {
		return agentstate.Credentials{}, err
	}

	payload, err := json.Marshal(bootstrapRequest{
		NodeName: config.NodeName,
		Version:  config.Version,
	})
	if err != nil {
		return agentstate.Credentials{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return agentstate.Credentials{}, err
	}
	request.Header.Set("Authorization", "Bearer "+config.EnrollmentToken)
	request.Header.Set("Content-Type", "application/json")

	if client == nil {
		client = &http.Client{
			Timeout: bootstrapRequestTimeout,
		}
	}

	response, err := client.Do(request)
	if err != nil {
		return agentstate.Credentials{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		message, err := readBootstrapError(response)
		if err != nil {
			return agentstate.Credentials{}, err
		}
		return agentstate.Credentials{}, errors.New("bootstrap failed: " + message)
	}

	var bootstrap bootstrapResponse
	if err := json.NewDecoder(response.Body).Decode(&bootstrap); err != nil {
		return agentstate.Credentials{}, err
	}
	if bootstrap.AgentID == "" || bootstrap.CertificatePEM == "" || bootstrap.PrivateKeyPEM == "" || bootstrap.CAPEM == "" || bootstrap.GRPCEndpoint == "" || bootstrap.GRPCServerName == "" {
		return agentstate.Credentials{}, errors.New("bootstrap response missing required fields")
	}

	credentialsState := agentstate.Credentials{
		AgentID:        bootstrap.AgentID,
		CertificatePEM: bootstrap.CertificatePEM,
		PrivateKeyPEM:  bootstrap.PrivateKeyPEM,
		CAPEM:          bootstrap.CAPEM,
		PanelURL:       strings.TrimRight(strings.TrimSpace(config.PanelURL), "/"),
		GRPCEndpoint:   bootstrap.GRPCEndpoint,
		GRPCServerName: bootstrap.GRPCServerName,
		// Persist the transport choice so certificate recovery later on
		// (see recoverRuntimeCredentialsIfNeeded) can honor it without
		// needing a CLI re-flag.
		InsecureTransport: config.InsecureTransport,
		// Carry the panel's attempt id forward so the next process
		// (runRuntime) can ship local timeline steps via
		// ReportEnrollmentSteps. Empty when the panel pre-dates Phase 1
		// enrollment logging — the reporter then becomes a no-op.
		EnrollmentAttemptID:  bootstrap.AttemptID,
		AgentPersistedCertAt: time.Now().UTC(),
	}
	if bootstrap.ExpiresAtUnix != 0 {
		credentialsState.ExpiresAt = time.Unix(bootstrap.ExpiresAtUnix, 0).UTC()
	}

	return credentialsState, nil
}

func bootstrapEndpointURL(panelURL string, allowInsecure bool) (string, error) {
	return agentEndpointURL(panelURL, agentBootstrapPath, allowInsecure)
}

func agentRecoveryEndpointURL(panelURL string, allowInsecure bool) (string, error) {
	return agentEndpointURL(panelURL, agentCertificateRecoveryPath, allowInsecure)
}

func agentEndpointURL(panelURL string, path string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(panelURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("bootstrap requires an absolute -panel-url")
	}

	// Gate order (most-to-least strict; Q2.U-S-05):
	//  1. https, or http on loopback           → always fine.
	//  2. http on a private IP literal + flag  → accepted with a warn
	//     log. Covers the "panel and agent share a VPN / LAN" case
	//     (10/8, 172.16/12, 192.168/16, CGNAT 100.64/10, IPv6 ULA
	//     fc00::/7, link-local fe80::/10, 169.254/16). The operator
	//     must still pass `-insecure-transport` so an accidental http://
	//     in config does not silently downgrade transport.
	//  3. http on a public IP / hostname + flag → accepted with the same
	//     flag. Audit U-S-05 keeps loopback as the only no-flag http path.
	switch {
	case panelURLUsesSecureTransport(parsed):
		// fine
	case panelURLHostIsPrivate(parsed) && allowInsecure:
		slog.Warn("bootstrap over http on private-network host",
			slog.String("host", parsed.Hostname()),
			slog.String("hint", "private key transits unencrypted; safe only if the link is trusted"))
	case allowInsecure:
		// fine — operator has taken responsibility via -insecure-transport.
	default:
		return "", errors.New("bootstrap requires https panel_url unless it targets loopback; pass -insecure-transport for any other http route, including private IPs")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func panelURLUsesSecureTransport(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		return true
	}
	if !strings.EqualFold(parsed.Scheme, "http") {
		return false
	}

	host := strings.TrimSpace(parsed.Hostname())
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// panelURLHostIsPrivate returns true when the panel URL host is a literal
// IP address inside a range that is not routable on the public internet:
// RFC1918 (10/8, 172.16/12, 192.168/16), CGNAT (100.64/10, commonly used
// by Tailscale), IPv4 link-local (169.254/16), IPv6 ULA (fc00::/7), or
// IPv6 link-local (fe80::/10). Loopback is intentionally excluded —
// `panelURLUsesSecureTransport` already accepts it.
//
// Hostnames are NOT resolved here: DNS is network-dependent and
// unreliable at bootstrap time. Operators using a hostname must pass
// `-insecure-transport` explicitly.
func panelURLHostIsPrivate(parsed *url.URL) bool {
	if parsed == nil || !strings.EqualFold(parsed.Scheme, "http") {
		return false
	}
	host := strings.TrimSpace(parsed.Hostname())
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		// Already accepted elsewhere.
		return false
	}
	if ip.IsPrivate() { // RFC1918 v4 + IPv6 ULA
		return true
	}
	if ip.IsLinkLocalUnicast() {
		return true
	}
	// CGNAT 100.64.0.0/10 — Go's IsPrivate() does not include it, but
	// Tailscale and most carriers assign out of this range for overlay
	// links that are functionally private.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && (v4[1]&0xC0) == 64 {
		return true
	}
	return false
}

func recoverRuntimeCredentialsIfNeeded(ctx context.Context, stateFile string, current agentstate.Credentials, client *http.Client, now time.Time) (agentstate.Credentials, error) {
	endpoint, err := agentRecoveryEndpointURL(current.PanelURL, current.InsecureTransport)
	if err != nil {
		return current, err
	}

	proofNonce, proofSignature, err := buildRecoveryProof(current.AgentID, current.PrivateKeyPEM, now.Unix())
	if err != nil {
		return current, err
	}

	payload, err := json.Marshal(agentCertificateRecoveryRequest{
		AgentID:            current.AgentID,
		CertificatePEM:     current.CertificatePEM,
		ProofTimestampUnix: now.Unix(),
		ProofNonce:         proofNonce,
		ProofSignature:     proofSignature,
	})
	if err != nil {
		return current, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return current, err
	}
	request.Header.Set("Content-Type", "application/json")

	if client == nil {
		client = &http.Client{
			Timeout: bootstrapRequestTimeout,
		}
	}

	response, err := client.Do(request)
	if err != nil {
		return current, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		message, err := readBootstrapError(response)
		if err != nil {
			return current, err
		}
		return current, errors.New("certificate recovery failed: " + message)
	}

	var recovery bootstrapResponse
	if err := json.NewDecoder(response.Body).Decode(&recovery); err != nil {
		return current, err
	}
	if recovery.AgentID == "" || recovery.CertificatePEM == "" || recovery.PrivateKeyPEM == "" || recovery.CAPEM == "" || recovery.GRPCEndpoint == "" || recovery.GRPCServerName == "" {
		return current, errors.New("certificate recovery response missing required fields")
	}

	updated := current
	updated.AgentID = recovery.AgentID
	updated.CertificatePEM = recovery.CertificatePEM
	updated.PrivateKeyPEM = recovery.PrivateKeyPEM
	updated.CAPEM = recovery.CAPEM
	updated.PanelURL = strings.TrimRight(strings.TrimSpace(current.PanelURL), "/")
	updated.GRPCEndpoint = recovery.GRPCEndpoint
	updated.GRPCServerName = recovery.GRPCServerName
	if recovery.ExpiresAtUnix > 0 {
		updated.ExpiresAt = time.Unix(recovery.ExpiresAtUnix, 0).UTC()
	} else {
		updated.ExpiresAt = time.Time{}
	}

	if err := agentstate.Save(stateFile, updated); err != nil {
		return current, err
	}

	return updated, nil
}

func buildRecoveryProof(agentID string, privateKeyPEM string, proofTimestampUnix int64) (string, string, error) {
	privateKey, err := parseRecoveryPrivateKey(privateKeyPEM)
	if err != nil {
		return "", "", err
	}

	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	payload := recoveryProofPayload(agentID, proofTimestampUnix, nonce)
	digest := sha256.Sum256([]byte(payload))
	signature, err := ecdsa.SignASN1(rand.Reader, privateKey, digest[:])
	if err != nil {
		return "", "", err
	}

	return nonce, base64.RawURLEncoding.EncodeToString(signature), nil
}

func parseRecoveryPrivateKey(privateKeyPEM string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, errors.New("failed to decode recovery private key")
	}

	privateKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err == nil {
		return privateKey, nil
	}

	parsedKey, pkcs8Err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if pkcs8Err != nil {
		return nil, err
	}

	ecdsaKey, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("recovery private key must be ECDSA")
	}

	return ecdsaKey, nil
}

func recoveryProofPayload(agentID string, proofTimestampUnix int64, proofNonce string) string {
	return agentID + "\n" + strconv.FormatInt(proofTimestampUnix, 10) + "\n" + proofNonce
}

func readBootstrapError(response *http.Response) (string, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		return payload.Error, nil
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		return response.Status, nil
	}

	return message, nil
}
