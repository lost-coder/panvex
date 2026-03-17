package telemt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var (
	// ErrNonLoopbackEndpoint reports a Telemt endpoint outside the local host boundary.
	ErrNonLoopbackEndpoint = errors.New("telemt endpoint must resolve to loopback")
)

// Config contains the local Telemt API location and authorization secret.
type Config struct {
	BaseURL       string
	Authorization string
}

// Client accesses the Telemt control API through a loopback-only endpoint.
type Client struct {
	baseURL       *url.URL
	authorization string
	httpClient    *http.Client
}

// RuntimeState summarizes the Telemt information the agent reports to the control-plane.
type RuntimeState struct {
	Version        string
	ReadOnly       bool
	ConnectedUsers int
	Clients        []ClientUsage
}

// ManagedClient stores the centrally managed Telemt client fields applied on one node.
type ManagedClient struct {
	PreviousName      string
	Name              string
	Secret            string
	UserADTag         string
	Enabled           bool
	MaxTCPConns       int
	MaxUniqueIPs      int
	DataQuotaBytes    int64
	ExpirationRFC3339 string
}

// ClientUsage summarizes one managed client's current usage on the local Telemt node.
type ClientUsage struct {
	ClientID         string
	ClientName       string
	TrafficUsedBytes uint64
	UniqueIPsUsed    int
	ActiveTCPConns   int
}

// ClientApplyResult stores the link material returned after Telemt applies a client.
type ClientApplyResult struct {
	ConnectionLink string
}

// NewClient validates the target endpoint and constructs a local-only Telemt client.
func NewClient(config Config, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, err
	}

	if !isLoopbackHost(parsed.Hostname()) {
		return nil, ErrNonLoopbackEndpoint
	}

	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		baseURL:       parsed,
		authorization: config.Authorization,
		httpClient:    httpClient,
	}, nil
}

func isLoopbackHost(host string) bool {
	normalized := strings.TrimSpace(strings.ToLower(host))
	switch normalized {
	case "localhost", "::1":
		return true
	}

	ip := net.ParseIP(normalized)
	return ip != nil && ip.IsLoopback()
}

// FetchRuntimeState queries the Telemt health, security posture, and summary endpoints.
func (c *Client) FetchRuntimeState(ctx context.Context) (RuntimeState, error) {
	health := struct {
		Version string `json:"version"`
	}{}
	if err := c.getJSON(ctx, "/v1/health", &health); err != nil {
		return RuntimeState{}, err
	}

	posture := struct {
		ReadOnly bool `json:"read_only"`
	}{}
	if err := c.getJSON(ctx, "/v1/security/posture", &posture); err != nil {
		return RuntimeState{}, err
	}

	summary := struct {
		ConnectedUsers int `json:"active_connections"`
	}{}
	if err := c.getJSON(ctx, "/v1/stats/summary", &summary); err != nil {
		return RuntimeState{}, err
	}

	users := make([]struct {
		Username           string `json:"username"`
		CurrentConnections int    `json:"current_connections"`
		ActiveUniqueIPs    int    `json:"active_unique_ips"`
		TotalOctets        uint64 `json:"total_octets"`
	}, 0)
	if err := c.getJSON(ctx, "/v1/users", &users); err != nil {
		return RuntimeState{}, err
	}

	clientUsage := make([]ClientUsage, 0, len(users))
	for _, user := range users {
		clientUsage = append(clientUsage, ClientUsage{
			ClientName:       user.Username,
			TrafficUsedBytes: user.TotalOctets,
			UniqueIPsUsed:    user.ActiveUniqueIPs,
			ActiveTCPConns:   user.CurrentConnections,
		})
	}

	return RuntimeState{
		Version:        health.Version,
		ReadOnly:       posture.ReadOnly,
		ConnectedUsers: summary.ConnectedUsers,
		Clients:        clientUsage,
	}, nil
}

// ExecuteRuntimeReload invokes the Telemt runtime reload endpoint.
func (c *Client) ExecuteRuntimeReload(ctx context.Context) error {
	request, err := c.newRequest(ctx, http.MethodPost, "/v1/runtime/reload", nil)
	if err != nil {
		return err
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("runtime reload failed with status %d", response.StatusCode)
	}

	return nil
}

// CreateClient creates one managed Telemt client and returns the preferred connection link.
func (c *Client) CreateClient(ctx context.Context, client ManagedClient) (ClientApplyResult, error) {
	return c.applyClient(ctx, http.MethodPost, "/v1/users", client)
}

// UpdateClient updates one managed Telemt client and returns the preferred connection link.
func (c *Client) UpdateClient(ctx context.Context, client ManagedClient) (ClientApplyResult, error) {
	targetName := client.Name
	if strings.TrimSpace(client.PreviousName) != "" {
		targetName = client.PreviousName
	}

	return c.applyClient(ctx, http.MethodPatch, "/v1/users/"+url.PathEscape(targetName), client)
}

// DeleteClient removes one managed Telemt client from the local Telemt node.
func (c *Client) DeleteClient(ctx context.Context, clientName string) error {
	request, err := c.newRequest(ctx, http.MethodDelete, "/v1/users/"+url.PathEscape(clientName), nil)
	if err != nil {
		return err
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("delete client failed with status %d", response.StatusCode)
	}

	return nil
}

func (c *Client) applyClient(ctx context.Context, method string, path string, client ManagedClient) (ClientApplyResult, error) {
	payload := map[string]any{
		"username":            client.Name,
		"secret":              client.Secret,
		"user_ad_tag":         client.UserADTag,
		"enabled":             client.Enabled,
		"max_tcp_conns":       client.MaxTCPConns,
		"max_unique_ips":      client.MaxUniqueIPs,
		"data_quota_bytes":    client.DataQuotaBytes,
		"expiration_rfc3339":  client.ExpirationRFC3339,
	}

	request, err := c.newRequest(ctx, method, path, payload)
	if err != nil {
		return ClientApplyResult{}, err
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return ClientApplyResult{}, err
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		return ClientApplyResult{}, fmt.Errorf("apply client failed with status %d", response.StatusCode)
	}

	var body struct {
		Links struct {
			TLS    []string `json:"tls"`
			Secure []string `json:"secure"`
			Classic []string `json:"classic"`
		} `json:"links"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return ClientApplyResult{}, err
	}

	return ClientApplyResult{
		ConnectionLink: preferredConnectionLink(body.Links.TLS, body.Links.Secure, body.Links.Classic),
	}, nil
}

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	request, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("telemt request failed with status %d", response.StatusCode)
	}

	return json.NewDecoder(response.Body).Decode(dest)
}

func (c *Client) newRequest(ctx context.Context, method string, path string, body any) (*http.Request, error) {
	endpoint := *c.baseURL
	endpoint.Path = path

	var requestBody *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		requestBody = bytes.NewReader(payload)
	} else {
		requestBody = bytes.NewReader(nil)
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Authorization", c.authorization)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	return request, nil
}

func preferredConnectionLink(tlsLinks []string, secureLinks []string, classicLinks []string) string {
	for _, candidate := range [][]string{tlsLinks, secureLinks, classicLinks} {
		if len(candidate) > 0 && strings.TrimSpace(candidate[0]) != "" {
			return candidate[0]
		}
	}

	return ""
}
