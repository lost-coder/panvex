package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	agentstate "github.com/panvex/panvex/internal/agent/state"
)

const agentBootstrapPath = "/api/agent/bootstrap"
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
		GRPCEndpoint:   bootstrap.GRPCEndpoint,
		GRPCServerName: bootstrap.GRPCServerName,
	}
	if bootstrap.ExpiresAtUnix != 0 {
		credentialsState.ExpiresAt = time.Unix(bootstrap.ExpiresAtUnix, 0).UTC()
	}

	return credentialsState, nil
}

func bootstrapEndpointURL(panelURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(panelURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("bootstrap requires an absolute -panel-url")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + agentBootstrapPath
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
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
