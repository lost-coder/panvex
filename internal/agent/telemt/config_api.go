package telemt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

var (
	// ErrReloadInProgress is returned when a concurrent Maestro reload is
	// non-terminal (HTTP 409 reload_in_progress). The caller retries with backoff.
	ErrReloadInProgress = errors.New("telemt: reload already in progress")
	// ErrMaestroUnavailable is returned when Maestro's reload channel is down
	// (HTTP 503 maestro_unavailable).
	ErrMaestroUnavailable = errors.New("telemt: maestro reload coordinator unavailable")
)

// ReloadAccepted is the 202 body of POST /v1/system/reload (and the "reload"
// block of PATCH /v1/config?reload=...). Metadata for the accepted operation.
type ReloadAccepted struct {
	ReloadID         uint64 `json:"reload_id"`
	TargetGeneration uint64 `json:"target_generation"`
	ConfigRevision   string `json:"config_revision"`
	State            string `json:"state"`
	Mode             string `json:"mode"`
	FailurePolicy    string `json:"failure_policy"`
}

// ReloadStatus is GET /v1/system/reload/{id}. State is one of accepted /
// preparing / activating / draining / succeeded / rolled_back / failed.
type ReloadStatus struct {
	ReloadID              uint64   `json:"reload_id"`
	State                 string   `json:"state"`
	FinishedAtEpochSecs   *uint64  `json:"finished_at_epoch_secs"`
	DeferredProcessFields []string `json:"deferred_process_fields"`
	Warnings              []string `json:"warnings"`
	Error                 string   `json:"error"`
}

// PatchConfigResult is Telemt's response to PATCH /v1/config.
type PatchConfigResult struct {
	Revision        string   `json:"revision"`
	RestartRequired bool     `json:"restart_required"` // legacy: only used to detect an old Telemt
	Changed         []string `json:"changed"`
	// RuntimeReloadRequired is a pointer on purpose: false is a valid value,
	// and a plain bool cannot tell "field absent" (old Telemt) from "field
	// present and false". The whole old-Telemt detection hangs on that.
	RuntimeReloadRequired  *bool    `json:"runtime_reload_required"`
	ProcessRestartRequired bool     `json:"process_restart_required"`
	DeferredProcessFields  []string `json:"deferred_process_fields"`
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

// SubmitReload submits an in-process Maestro reload via POST /v1/system/reload.
// mode is "instant" or "drain"; timeoutSecs is required (and only valid) for
// drain. failurePolicy is "rollback" for a forward apply or "keep_new" for the
// rollback-reload. ifMatchRevision is the revision the caller just wrote, sent
// as If-Match.
func (c *Client) SubmitReload(ctx context.Context, mode string, timeoutSecs int, failurePolicy, ifMatchRevision string) (ReloadAccepted, error) {
	request, err := c.newRequest(ctx, http.MethodPost, "/v1/system/reload", nil)
	if err != nil {
		return ReloadAccepted{}, err
	}
	// newRequest sets endpoint.Path only — attach the query here so it survives.
	q := url.Values{}
	q.Set("reload", mode)
	if mode == "drain" {
		q.Set("timeout_secs", strconv.Itoa(timeoutSecs))
	}
	q.Set("failure_policy", failurePolicy)
	request.URL.RawQuery = q.Encode()
	if ifMatchRevision != "" {
		request.Header.Set("If-Match", ifMatchRevision)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return ReloadAccepted{}, err
	}
	defer response.Body.Close()

	// 409 carries two distinct codes: revision_conflict vs reload_in_progress.
	// Read the body ONCE into a buffer so both the code-peek and the fallback
	// decodeAPIError see it (response.Body is not re-readable).
	if response.StatusCode == http.StatusServiceUnavailable {
		return ReloadAccepted{}, ErrMaestroUnavailable
	}
	if response.StatusCode >= http.StatusBadRequest {
		buf, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBodySize))
		code := apiErrorCode(bytes.NewReader(buf))
		if response.StatusCode == http.StatusConflict {
			if code == "revision_conflict" {
				return ReloadAccepted{}, ErrConfigRevisionConflict
			}
			return ReloadAccepted{}, ErrReloadInProgress
		}
		return ReloadAccepted{}, fmt.Errorf("submit reload failed: %w", decodeAPIError(bytes.NewReader(buf), fmt.Sprintf("submit reload failed with status %d", response.StatusCode)))
	}
	var acc ReloadAccepted
	if err := decodeSuccessData(response.Body, &acc); err != nil {
		return ReloadAccepted{}, err
	}
	return acc, nil
}

// GetReloadStatus polls one reload operation via GET /v1/system/reload/{id}.
func (c *Client) GetReloadStatus(ctx context.Context, reloadID uint64) (ReloadStatus, error) {
	path := fmt.Sprintf("/v1/system/reload/%d", reloadID)
	request, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return ReloadStatus{}, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return ReloadStatus{}, err
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		return ReloadStatus{}, fmt.Errorf("get reload status failed: %w", decodeAPIError(response.Body, fmt.Sprintf("get reload status failed with status %d", response.StatusCode)))
	}
	var st ReloadStatus
	if err := decodeSuccessData(response.Body, &st); err != nil {
		return ReloadStatus{}, err
	}
	return st, nil
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
