package telemt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrResetQuotaUnsupported is returned by ResetUserQuota when the local
// Telemt build predates the POST /v1/users/{u}/reset-quota endpoint
// (introduced in Telemt 3.4.6). Detected via HTTP 404 on the route
// itself — Telemt returns 404 for unknown routes even with a known
// username. The control-plane can match this typed error to render a
// "Reset unavailable (Telemt < 3.4.6)" affordance per-agent instead of
// a generic transport failure.
var ErrResetQuotaUnsupported = errors.New("telemt: reset-quota endpoint not available on this version")

// ErrResetQuotaReadOnly is returned by ResetUserQuota when Telemt is
// running in API read-only mode and rejects the mutation (HTTP 403).
// The panel surfaces this distinctly from a transport failure because
// the operator-actionable remedy is different (lift read-only vs. fix
// connectivity).
var ErrResetQuotaReadOnly = errors.New("telemt: API is in read-only mode")

// ErrClientNotFound is returned by DeleteClient / UpdateClient when
// Telemt reports HTTP 404 for the target user. Callers use it to make
// operations idempotent: deleting an already-absent user is a no-op
// success (disable path), and patching a missing user can fall back to
// a create (re-enable / drift-heal path).
var ErrClientNotFound = errors.New("telemt: user not found")

// ErrClientAlreadyExists is returned by CreateClient when Telemt answers
// 409 user_exists — the username is already configured on the node. The
// client payload is a full desired-state upsert, so callers converge by
// falling back to the PATCH path (audit F6: a redelivered create after a
// lost ack must not fail forever).
var ErrClientAlreadyExists = errors.New("telemt: user already exists")

// FetchActiveIPs fetches the /v1/stats/users/active-ips endpoint and returns per-user active IPs.
func (c *Client) FetchActiveIPs(ctx context.Context) ([]UserActiveIPs, error) {
	var users []UserActiveIPs
	if err := c.getJSON(ctx, "/v1/stats/users/active-ips", &users); err != nil {
		return nil, err
	}
	c.logger.Debug(logTelemtAPICall, "path", "/v1/stats/users/active-ips", "user_count", len(users))

	return users, nil
}

// CreateClient creates one managed Telemt client and returns the preferred connection link.
func (c *Client) CreateClient(ctx context.Context, client ManagedClient) (ClientApplyResult, error) {
	return c.applyClient(ctx, http.MethodPost, "/v1/users", client)
}

// UpdateClient updates one managed Telemt client and returns the preferred
// connection link. The PATCH always targets client.Name — the name is
// immutable panel-side because Telemt has no rename operation (audit F2).
func (c *Client) UpdateClient(ctx context.Context, client ManagedClient) (ClientApplyResult, error) {
	return c.applyClient(ctx, http.MethodPatch, "/v1/users/"+url.PathEscape(client.Name), client)
}

// ResetUserQuotaResult carries the post-reset quota snapshot Telemt
// emits at POST /v1/users/{u}/reset-quota.
type ResetUserQuotaResult struct {
	Username           string
	UsedBytes          uint64
	LastResetEpochSecs uint64
}

// ResetUserQuota resets the resettable quota counter (used_bytes) for a
// single Telemt user. The endpoint was introduced in Telemt 3.4.6; on
// older builds the route returns 404 and we surface ErrResetQuotaUnsupported
// so the caller can distinguish "operator needs to upgrade Telemt" from
// "network glitch / retry". HTTP 403 surfaces as ErrResetQuotaReadOnly.
func (c *Client) ResetUserQuota(ctx context.Context, username string) (ResetUserQuotaResult, error) {
	path := "/v1/users/" + url.PathEscape(username) + "/reset-quota"
	request, err := c.newRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return ResetUserQuotaResult{}, err
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return ResetUserQuotaResult{}, err
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusNotFound:
		// Two distinct 404s share the "not_found" error code (audit F5):
		// the route handler's "User not found" (user absent from the node
		// — rename/delete raced the job) vs the dispatcher's "Route not
		// found" (Telemt < 3.4.6, endpoint absent). Only the latter means
		// the feature is unsupported; conflating them told operators
		// "Telemt too old" for a merely-missing user.
		apiErr := decodeAPIError(response.Body, "reset user quota failed with status 404")
		if strings.Contains(apiErr.Error(), "User not found") {
			return ResetUserQuotaResult{}, fmt.Errorf("reset user quota: %w", ErrClientNotFound)
		}
		return ResetUserQuotaResult{}, ErrResetQuotaUnsupported
	case http.StatusForbidden:
		return ResetUserQuotaResult{}, ErrResetQuotaReadOnly
	}
	if response.StatusCode >= http.StatusBadRequest {
		return ResetUserQuotaResult{}, fmt.Errorf("reset user quota failed: %w", decodeAPIError(response.Body, fmt.Sprintf("reset user quota failed with status %d", response.StatusCode)))
	}

	var body struct {
		Username           string `json:"username"`
		UsedBytes          uint64 `json:"used_bytes"`
		LastResetEpochSecs uint64 `json:"last_reset_epoch_secs"`
	}
	if err := decodeSuccessData(response.Body, &body); err != nil {
		return ResetUserQuotaResult{}, err
	}

	return ResetUserQuotaResult{
		Username:           body.Username,
		UsedBytes:          body.UsedBytes,
		LastResetEpochSecs: body.LastResetEpochSecs,
	}, nil
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

	if response.StatusCode == http.StatusNotFound {
		return ErrClientNotFound
	}
	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("delete client failed: %w", decodeAPIError(response.Body, fmt.Sprintf("delete client failed with status %d", response.StatusCode)))
	}

	return nil
}

func (c *Client) applyClient(ctx context.Context, method string, path string, client ManagedClient) (ClientApplyResult, error) {
	isPatch := method == http.MethodPatch
	payload := map[string]any{
		"secret": client.Secret,
	}
	// Telemt's PatchUserRequest has no username field (there is no rename
	// operation anywhere in its API — audit F2), so the name is only sent
	// on create; the PATCH target user is identified by the URL path.
	if !isPatch {
		payload["username"] = client.Name
	}
	// Panvex always ships the FULL desired client state on every apply,
	// so the optional fields are mapped to two distinct wire encodings:
	//
	//   - POST /v1/users (create): Telemt models the optionals as
	//     Option<…>. Sending "" for user_ad_tag triggers a 32-hex
	//     validation error and sending 0 for a numeric limit materialises
	//     a real zero-limit, so a cleared field must be *omitted* (= "no
	//     value / no limit").
	//   - PATCH /v1/users/{name} (update): Telemt uses JSON-Merge-Patch
	//     tri-state — an omitted field means Unchanged (keep the old
	//     value), explicit null means Remove. Because the panel sends the
	//     complete desired state, a cleared field must be sent as explicit
	//     null so it is actually removed; omitting it would silently
	//     preserve the stale value on the node.
	setOptionalString := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			payload[key] = value
		} else if isPatch {
			payload[key] = nil // explicit null → Telemt Patch::Remove
		}
	}
	setOptionalInt := func(key string, value int) {
		if value > 0 {
			payload[key] = value
		} else if isPatch {
			payload[key] = nil
		}
	}
	setOptionalInt64 := func(key string, value int64) {
		if value > 0 {
			payload[key] = value
		} else if isPatch {
			payload[key] = nil
		}
	}
	setOptionalString("user_ad_tag", client.UserADTag)
	setOptionalInt("max_tcp_conns", client.MaxTCPConns)
	setOptionalInt("max_unique_ips", client.MaxUniqueIPs)
	setOptionalInt64("data_quota_bytes", client.DataQuotaBytes)
	setOptionalString("expiration_rfc3339", client.ExpirationRFC3339)

	request, err := c.newRequest(ctx, method, path, payload)
	if err != nil {
		return ClientApplyResult{}, err
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return ClientApplyResult{}, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return ClientApplyResult{}, ErrClientNotFound
	}
	if response.StatusCode == http.StatusConflict {
		// Telemt's create handler answers 409 code "user_exists" when the
		// username is already configured (users.rs). The job payload is a
		// full desired-state upsert, so callers use this sentinel to
		// converge via the PATCH path instead of failing forever on a
		// redelivered create (audit F6). Other 409s (e.g.
		// last_user_forbidden) stay generic.
		apiErr := decodeAPIError(response.Body, "apply client failed with status 409")
		if strings.Contains(apiErr.Error(), "user_exists") {
			return ClientApplyResult{}, fmt.Errorf("apply client: %w", ErrClientAlreadyExists)
		}
		return ClientApplyResult{}, fmt.Errorf("apply client failed: %w", apiErr)
	}
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

	// IN-M1 (corrected): Telemt answers 202 ACCEPTED when the user was
	// persisted to disk but the runtime snapshot has not caught up yet.
	// Telemt activates the change ITSELF: an inotify watcher on the config
	// file applies it within ~50ms (telemt src/config/hot_reload.rs). There
	// is NO HTTP reload endpoint — Telemt's only "reload" is a CLI
	// subcommand that sends SIGHUP to the process. The original IN-M1 fix
	// invented a POST /v1/runtime/reload call, which 404'd and turned every
	// pre-inotify 202 into a spurious deployment failure. 202 already
	// carries the connection links exactly like 201, so it is plain success:
	// no extra handling needed here.

	return ClientApplyResult{
		ConnectionLinks: collectConnectionLinks(links.TLS, links.Secure, links.Classic),
	}, nil
}
