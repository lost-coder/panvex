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
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	agentstate "github.com/panvex/panvex/internal/agent/state"
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
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *panelURL == "" || *enrollmentToken == "" || *stateFile == "" {
		return errors.New("bootstrap requires -panel-url, -enrollment-token, and -state-file")
	}

	if !*force {
		if _, err := os.Stat(*stateFile); err == nil {
			return errors.New("bootstrap requires -force when the state file already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	credentialsState, err := bootstrapAgent(context.Background(), client, bootstrapConfig{
		PanelURL:        *panelURL,
		EnrollmentToken: *enrollmentToken,
		StateFile:       *stateFile,
		NodeName:        *nodeName,
		Version:         *version,
		Force:           *force,
	})
	if err != nil {
		return err
	}

	return agentstate.Save(*stateFile, credentialsState)
}

func bootstrapAgent(ctx context.Context, client *http.Client, config bootstrapConfig) (agentstate.Credentials, error) {
	endpoint, err := bootstrapEndpointURL(config.PanelURL)
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
	}
	if bootstrap.ExpiresAtUnix != 0 {
		credentialsState.ExpiresAt = time.Unix(bootstrap.ExpiresAtUnix, 0).UTC()
	}

	return credentialsState, nil
}

func bootstrapEndpointURL(panelURL string) (string, error) {
	return agentEndpointURL(panelURL, agentBootstrapPath)
}

func agentRecoveryEndpointURL(panelURL string) (string, error) {
	return agentEndpointURL(panelURL, agentCertificateRecoveryPath)
}

func agentEndpointURL(panelURL string, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(panelURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("bootstrap requires an absolute -panel-url")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func recoverRuntimeCredentialsIfNeeded(ctx context.Context, stateFile string, current agentstate.Credentials, client *http.Client, now time.Time) (agentstate.Credentials, error) {
	endpoint, err := agentRecoveryEndpointURL(current.PanelURL)
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
