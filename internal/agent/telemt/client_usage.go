package telemt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// pathUsersQuota is the Telemt 3.4.12+ endpoint that lists per-user
// data_quota_bytes / used_bytes / last_reset_epoch_secs for every user
// with a quota configured. Older Telemt builds return 404 here; callers
// MUST treat 404 as "no quota data available" rather than a hard error.
const pathUsersQuota = "/v1/users/quota"

// fetchUserQuotaInfo reads the per-user quota view from Telemt and
// returns one UserQuotaInfo entry per user with data_quota_bytes > 0.
//
// On 404 (Telemt < 3.4.12 — endpoint absent) the function returns an
// empty slice and a nil error so the surrounding usage build can still
// emit a complete snapshot with QuotaUsedBytes/QuotaLastResetUnix left
// at zero. Any other transport-level or non-2xx response is surfaced
// to the caller as an error.
func (c *Client) fetchUserQuotaInfo(ctx context.Context) ([]UserQuotaInfo, error) {
	var resp userQuotaResponse
	status, err := c.getJSONWithStatus(ctx, pathUsersQuota, &resp)
	if err != nil {
		if status == http.StatusNotFound {
			// Endpoint absent on older Telemt — expected case, not a failure.
			c.logger.Debug(logTelemtAPICall, "path", pathUsersQuota, "status", status, "note", "endpoint absent on older telemt")
			return nil, nil
		}
		return nil, err
	}
	out := make([]UserQuotaInfo, 0, len(resp.Users))
	for _, u := range resp.Users {
		out = append(out, UserQuotaInfo{
			Username:           u.Username,
			DataQuotaBytes:     u.DataQuotaBytes,
			UsedBytes:          u.UsedBytes,
			LastResetEpochSecs: u.LastResetEpochSecs,
		})
	}
	c.logger.Debug(logTelemtAPICall, "path", pathUsersQuota, "user_count", len(out))
	return out, nil
}

// mergeUserQuotaInfo overwrites the Quota* fields of every ClientUsage
// row whose ClientName matches a UserQuotaInfo entry. Users absent from
// quotas (no data_quota_bytes set) keep zeroed quota fields. The function
// mutates rows in place — callers pass the slice they're about to return.
func mergeUserQuotaInfo(rows []ClientUsage, quotas []UserQuotaInfo) {
	if len(quotas) == 0 || len(rows) == 0 {
		return
	}
	byName := make(map[string]UserQuotaInfo, len(quotas))
	for _, q := range quotas {
		byName[q.Username] = q
	}
	for i := range rows {
		if q, ok := byName[rows[i].ClientName]; ok {
			rows[i].QuotaUsedBytes = q.UsedBytes
			rows[i].QuotaLastResetUnix = q.LastResetEpochSecs
		}
	}
}

func (c *Client) fetchClientUsage(ctx context.Context) ([]ClientUsage, error) {
	users := make([]struct {
		Username           string `json:"username"`
		CurrentConnections int    `json:"current_connections"`
		ActiveUniqueIPs    int    `json:"active_unique_ips"`
		RecentUniqueIPs    int    `json:"recent_unique_ips"`
		TotalOctets        uint64 `json:"total_octets"`
	}, 0)
	if err := c.getJSON(ctx, "/v1/stats/users", &users); err != nil {
		return nil, err
	}

	clientUsage := make([]ClientUsage, 0, len(users))
	for _, user := range users {
		clientUsage = append(clientUsage, ClientUsage{
			ClientName:       user.Username,
			TrafficUsedBytes: user.TotalOctets,
			UniqueIPsUsed:    user.RecentUniqueIPs,
			CurrentIPsUsed:   user.ActiveUniqueIPs,
			ActiveTCPConns:   user.CurrentConnections,
		})
	}

	// Merge per-user quota info (Telemt 3.4.12+). Quiet on 404 / soft
	// error so the core usage snapshot remains intact when Telemt is
	// too old to expose the endpoint or transiently rejects the call;
	// downstream consumers see zeroed Quota* fields in those cases,
	// which the panel renders as "no quota data".
	if quotas, err := c.fetchUserQuotaInfo(ctx); err == nil {
		mergeUserQuotaInfo(clientUsage, quotas)
	} else {
		c.logger.Warn("telemt user quota fetch failed", "path", pathUsersQuota, "err", err)
	}

	return clientUsage, nil
}

type ClientUsageMetricsSnapshot struct {
	Users         []ClientUsage
	UptimeSeconds float64
}

// FetchClientUsageFromMetrics fetches the Prometheus /metrics endpoint and returns per-client usage.
// metricsURL must be configured; returns an error if it is nil.
func (c *Client) FetchClientUsageFromMetrics(ctx context.Context) (ClientUsageMetricsSnapshot, error) {
	if c.metricsURL == nil {
		return ClientUsageMetricsSnapshot{}, errors.New("telemt metrics endpoint is not configured")
	}

	endpoint := *c.metricsURL
	endpoint.Path = "/metrics"

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return ClientUsageMetricsSnapshot{}, err
	}
	request.Header.Set("Authorization", c.authorization)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return ClientUsageMetricsSnapshot{}, err
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		return ClientUsageMetricsSnapshot{}, fmt.Errorf("telemt metrics request failed with status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return ClientUsageMetricsSnapshot{}, err
	}

	parsed := ParseMetricsSnapshot(string(body))
	if c.upstreamRate != nil {
		c.upstreamRate.Push(time.Now(), parsed.UpstreamCounters)
	}
	c.upstreamCountersMu.Lock()
	c.latestUpstreamCounters = parsed.UpstreamCounters
	c.hasUpstreamCounters = true
	c.upstreamCountersMu.Unlock()
	c.logger.Debug(logTelemtAPICall, "path", "/metrics", "user_count", len(parsed.Users))
	result := make([]ClientUsage, 0, len(parsed.Users))
	for username, m := range parsed.Users {
		result = append(result, ClientUsage{
			ClientName:       username,
			TrafficUsedBytes: m.OctetsFromClient + m.OctetsToClient,
			ActiveTCPConns:   m.CurrentConnections,
			CurrentIPsUsed:   m.UniqueIPsCurrent,
			// IN-H3: "unique used" = distinct IPs over the observation window
			// (recent_window), distinct from active-now (CurrentIPsUsed).
			// Previously the metrics path left this 0, which then overwrote
			// the panel's per-client UniqueIPsUsed to 0 on every tick.
			UniqueIPsUsed: m.UniqueIPsRecentWindow,
		})
	}

	// Merge per-user quota / last-reset info from /v1/users/quota
	// (Telemt 3.4.12+). The endpoint is cheap (one small JSON list);
	// a 404 from older Telemt is swallowed inside fetchUserQuotaInfo
	// and surfaces as an empty slice here, leaving the Quota* fields
	// zeroed. Any other transport-level failure is non-fatal — we
	// still emit the metrics snapshot so traffic deltas keep flowing.
	if quotas, err := c.fetchUserQuotaInfo(ctx); err == nil {
		mergeUserQuotaInfo(result, quotas)
	} else {
		c.logger.Warn("telemt user quota fetch failed", "path", pathUsersQuota, "err", err)
	}

	return ClientUsageMetricsSnapshot{
		Users:         result,
		UptimeSeconds: parsed.UptimeSeconds,
	}, nil
}
