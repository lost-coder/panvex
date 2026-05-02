package telemt

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// FetchActiveIPs fetches the /v1/stats/users/active-ips endpoint and returns per-user active IPs.
func (c *Client) FetchActiveIPs(ctx context.Context) ([]UserActiveIPs, error) {
	var users []UserActiveIPs
	if err := c.getJSON(ctx, "/v1/stats/users/active-ips", &users); err != nil {
		return nil, err
	}
	c.logger.Debug(logTelemtAPICall, "path", "/v1/stats/users/active-ips", "user_count", len(users))

	return users, nil
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
		return fmt.Errorf("runtime reload failed: %w", decodeAPIError(response.Body, fmt.Sprintf("runtime reload failed with status %d", response.StatusCode)))
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
		return fmt.Errorf("delete client failed: %w", decodeAPIError(response.Body, fmt.Sprintf("delete client failed with status %d", response.StatusCode)))
	}

	return nil
}

func (c *Client) applyClient(ctx context.Context, method string, path string, client ManagedClient) (ClientApplyResult, error) {
	payload := map[string]any{
		"username": client.Name,
		"secret":   client.Secret,
		"enabled":  client.Enabled,
	}
	// Telemt models user_ad_tag as Option<String>: omitting the field
	// means "no ad tag", while sending "" triggers a 32-hex validation
	// error. Include the field only when the operator actually provided
	// a value.
	if strings.TrimSpace(client.UserADTag) != "" {
		payload["user_ad_tag"] = client.UserADTag
	}
	// Telemt's CreateUserRequest models the numeric limits as
	// `Option<usize>` — sending `0` materialises a real zero-limit
	// (the client then can't open any connections, burn any quota,
	// etc.), while *omitting* the field means "no limit". Map zero
	// values to "no limit" so operators who leave the form blank get
	// the expected unlimited client instead of a silently-broken one.
	if client.MaxTCPConns > 0 {
		payload["max_tcp_conns"] = client.MaxTCPConns
	}
	if client.MaxUniqueIPs > 0 {
		payload["max_unique_ips"] = client.MaxUniqueIPs
	}
	if client.DataQuotaBytes > 0 {
		payload["data_quota_bytes"] = client.DataQuotaBytes
	}
	if strings.TrimSpace(client.ExpirationRFC3339) != "" {
		payload["expiration_rfc3339"] = client.ExpirationRFC3339
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
		return ClientApplyResult{}, fmt.Errorf("apply client failed: %w", decodeAPIError(response.Body, fmt.Sprintf("apply client failed with status %d", response.StatusCode)))
	}

	// Telemt returns two shapes depending on the HTTP method:
	//   POST /v1/users         → {"data":{"user":{"links":{…}}, "secret":…}}  (CreateUserResponse)
	//   PATCH /v1/users/{name} → {"data":{"links":{…}, …}}                    (UserInfo)
	// Decode both nesting levels and pick whichever branch is populated.
	// Unknown fields are silently ignored by encoding/json, so a single
	// struct captures whichever Telemt shipped.
	type linksBlock struct {
		TLS     []string `json:"tls"`
		Secure  []string `json:"secure"`
		Classic []string `json:"classic"`
	}
	var body struct {
		Links linksBlock `json:"links"`
		User  struct {
			Links linksBlock `json:"links"`
		} `json:"user"`
	}
	if err := decodeSuccessData(response.Body, &body); err != nil {
		return ClientApplyResult{}, err
	}

	links := body.Links
	if len(links.TLS) == 0 && len(links.Secure) == 0 && len(links.Classic) == 0 {
		links = body.User.Links
	}

	return ClientApplyResult{
		ConnectionLinks: collectConnectionLinks(links.TLS, links.Secure, links.Classic),
	}, nil
}
