package telemt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)


// ErrConfigEditUnsupported is returned when the local Telemt build predates the
// PATCH/GET /v1/config endpoints (HTTP 404 on the route). Lets the panel render
// "Config editing unavailable (upgrade Telemt)" per-node instead of a transport error.
var ErrConfigEditUnsupported = errors.New("telemt: config-edit endpoint not available on this version")

// ErrConfigEditReadOnly is returned when Telemt's API is in read-only mode (HTTP 403).
var ErrConfigEditReadOnly = errors.New("telemt: API is in read-only mode")

// ErrConfigRevisionConflict is returned when the If-Match revision did not match
// the on-disk config (HTTP 409). The caller re-reads the current config and retries.
var ErrConfigRevisionConflict = errors.New("telemt: config revision conflict")

// PatchConfigResult is Telemt's response to PATCH /v1/config.
type PatchConfigResult struct {
	Revision        string   `json:"revision"`
	RestartRequired bool     `json:"restart_required"`
	Changed         []string `json:"changed"`
}

// PatchConfig applies a sparse config patch via Telemt's PATCH /v1/config.
// expectedRevision, when non-empty, is sent as the If-Match header for optimistic
// concurrency. Hot-reloadable fields take effect immediately (Telemt's file
// watcher); when RestartRequired is true the caller must restart the process.
func (c *Client) PatchConfig(ctx context.Context, patch map[string]any, expectedRevision string) (PatchConfigResult, error) {
	request, err := c.newRequest(ctx, http.MethodPatch, "/v1/config", patch)
	if err != nil {
		return PatchConfigResult{}, err
	}
	if expectedRevision != "" {
		request.Header.Set("If-Match", expectedRevision)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return PatchConfigResult{}, err
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusNotFound:
		return PatchConfigResult{}, ErrConfigEditUnsupported
	case http.StatusForbidden:
		return PatchConfigResult{}, ErrConfigEditReadOnly
	case http.StatusConflict:
		return PatchConfigResult{}, ErrConfigRevisionConflict
	}
	if response.StatusCode >= http.StatusBadRequest {
		return PatchConfigResult{}, fmt.Errorf("patch config failed: %w", decodeAPIError(response.Body, fmt.Sprintf("patch config failed with status %d", response.StatusCode)))
	}

	var result PatchConfigResult
	if err := decodeSuccessData(response.Body, &result); err != nil {
		return PatchConfigResult{}, err
	}
	return result, nil
}

// GetManagedConfig fetches the editable config sections (access stripped) and the
// current revision via GET /v1/config. Sections are returned as a generic map so
// the agent forwards them verbatim without modeling every Telemt field.
func (c *Client) GetManagedConfig(ctx context.Context) (map[string]any, string, error) {
	request, err := c.newRequest(ctx, http.MethodGet, "/v1/config", nil)
	if err != nil {
		return nil, "", err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return nil, "", ErrConfigEditUnsupported
	}
	if response.StatusCode >= http.StatusBadRequest {
		return nil, "", fmt.Errorf("get config failed: %w", decodeAPIError(response.Body, fmt.Sprintf("get config failed with status %d", response.StatusCode)))
	}

	var envelope struct {
		Data     map[string]any `json:"data"`
		Revision string         `json:"revision"`
	}
	if err := decodeJSONBody(response.Body, &envelope); err != nil {
		return nil, "", err
	}
	return envelope.Data, envelope.Revision, nil
}

// HealthReady reports whether Telemt is ready to serve (GET /v1/health/ready).
// 200 => ready; 503 => not ready (with a reason); other => error.
func (c *Client) HealthReady(ctx context.Context) (bool, string, error) {
	request, err := c.newRequest(ctx, http.MethodGet, "/v1/health/ready", nil)
	if err != nil {
		return false, "", err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return false, "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusServiceUnavailable {
		return false, "", fmt.Errorf("health ready failed with status %d", response.StatusCode)
	}

	var envelope struct {
		Data struct {
			Ready  bool   `json:"ready"`
			Reason string `json:"reason"`
		} `json:"data"`
	}
	if err := decodeJSONBody(response.Body, &envelope); err != nil {
		return false, "", err
	}
	return envelope.Data.Ready, envelope.Data.Reason, nil
}
