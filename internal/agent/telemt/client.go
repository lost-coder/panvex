package telemt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNonLoopbackEndpoint reports a Telemt endpoint outside the local host boundary.
	ErrNonLoopbackEndpoint = errors.New("telemt endpoint must resolve to loopback")
)

// defaultSlowDataTTL bounds staleness for heavier Telemt endpoints while reducing repeated local reads.
const defaultSlowDataTTL = 2 * time.Minute

// defaultRequestTimeout bounds local Telemt API calls to prevent indefinite request hangs.
const defaultRequestTimeout = 15 * time.Second

// Config contains the local Telemt API location and authorization secret.
type Config struct {
	BaseURL       string
	MetricsURL    string
	Authorization string
}

// Client accesses the Telemt control API through a loopback-only endpoint.
type Client struct {
	baseURL       *url.URL
	metricsURL    *url.URL
	authorization string
	httpClient    *http.Client
	mu            sync.RWMutex
	slowDataTTL   time.Duration
	slowFetchedAt time.Time
	slowData      slowRuntimeState
	hasSlowData   bool
}

// InvalidateSlowDataCache forces the next runtime snapshot to refetch slow diagnostics.
func (c *Client) InvalidateSlowDataCache() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.hasSlowData = false
	c.slowFetchedAt = time.Time{}
	c.slowData = slowRuntimeState{}
}

// slowRuntimeState stores data from heavier Telemt endpoints that tolerate short-lived staleness.
type slowRuntimeState struct {
	Version       string
	UptimeSeconds float64
	Upstreams     RuntimeUpstreamSummary
	RecentEvents  []RuntimeEvent
	Diagnostics   RuntimeDiagnostics
	SecurityInventory RuntimeSecurityInventory
}

// RuntimeState summarizes the Telemt information the agent reports to the control-plane.
type RuntimeState struct {
	Version          string
	ReadOnly         bool
	UptimeSeconds    float64
	ConnectedUsers   int
	Gates            RuntimeGates
	Initialization   RuntimeInitialization
	ConnectionTotals RuntimeConnectionTotals
	Summary          RuntimeSummary
	DCs              []RuntimeDC
	Upstreams        RuntimeUpstreamSummary
	RecentEvents     []RuntimeEvent
	Diagnostics      RuntimeDiagnostics
	SecurityInventory RuntimeSecurityInventory
	Clients          []ClientUsage
}

// RuntimeDiagnostics carries slower Telemt diagnostics payloads for node detail views.
type RuntimeDiagnostics struct {
	State               string
	StateReason         string
	SystemInfoJSON      string
	EffectiveLimitsJSON string
	SecurityPostureJSON string
	MinimalAllJSON      string
	MEPoolJSON          string
}

// RuntimeSecurityInventory carries whitelist inventory data used by security detail sections.
type RuntimeSecurityInventory struct {
	State        string
	StateReason  string
	Enabled      bool
	EntriesTotal int
	EntriesJSON  string
}

// RuntimeGates carries the operator-facing admission and transport gates.
type RuntimeGates struct {
	AcceptingNewConnections bool
	MERuntimeReady          bool
	ME2DCFallbackEnabled    bool
	UseMiddleProxy          bool
	StartupStatus           string
	StartupStage            string
	StartupProgressPct      float64
}

// RuntimeInitialization carries the current startup and degraded-mode state.
type RuntimeInitialization struct {
	Status        string
	Degraded      bool
	CurrentStage  string
	ProgressPct   float64
	TransportMode string
}

// RuntimeConnectionTotals carries the current live connection split.
type RuntimeConnectionTotals struct {
	CurrentConnections       int
	CurrentConnectionsME     int
	CurrentConnectionsDirect int
	ActiveUsers              int
}

// RuntimeSummary carries cumulative connection counters used for overview cards.
type RuntimeSummary struct {
	ConnectionsTotal       uint64
	ConnectionsBadTotal    uint64
	HandshakeTimeoutsTotal uint64
	ConfiguredUsers        int
}

// RuntimeDC carries one operator-facing DC health row.
type RuntimeDC struct {
	DC                 int
	AvailableEndpoints int
	AvailablePct       float64
	RequiredWriters    int
	AliveWriters       int
	CoveragePct        float64
	RTTMs              float64
	Load               int
}

// RuntimeUpstreamSummary carries the upstream health overview.
type RuntimeUpstreamSummary struct {
	ConfiguredTotal int
	HealthyTotal    int
	UnhealthyTotal  int
	DirectTotal     int
	SOCKS5Total     int
	Rows            []RuntimeUpstream
}

// RuntimeUpstream carries one operator-facing upstream row.
type RuntimeUpstream struct {
	UpstreamID         int
	RouteKind          string
	Address            string
	Healthy            bool
	Fails              int
	EffectiveLatencyMs float64
}

// RuntimeEvent carries one recent runtime event from Telemt.
type RuntimeEvent struct {
	Sequence      uint64
	TimestampUnix int64
	EventType     string
	Context       string
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
	CurrentIPsUsed   int
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

	var metricsURL *url.URL
	if strings.TrimSpace(config.MetricsURL) != "" {
		metricsURL, err = url.Parse(config.MetricsURL)
		if err != nil {
			return nil, err
		}
		if !isLoopbackHost(metricsURL.Hostname()) {
			return nil, ErrNonLoopbackEndpoint
		}
	}

	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: defaultRequestTimeout,
		}
	}

	return &Client{
		baseURL:       parsed,
		metricsURL:    metricsURL,
		authorization: config.Authorization,
		httpClient:    httpClient,
		slowDataTTL:   defaultSlowDataTTL,
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
		Status string `json:"status"`
	}{}
	if err := c.getJSON(ctx, "/v1/health", &health); err != nil {
		return RuntimeState{}, err
	}

	posture := struct {
		ReadOnly               bool   `json:"read_only"`
		APIReadOnly            bool   `json:"api_read_only"`
		APIWhitelistEnabled    bool   `json:"api_whitelist_enabled"`
		APIWhitelistEntries    int    `json:"api_whitelist_entries"`
		APIAuthHeaderEnabled   bool   `json:"api_auth_header_enabled"`
		ProxyProtocolEnabled   bool   `json:"proxy_protocol_enabled"`
		LogLevel               string `json:"log_level"`
		TelemetryCoreEnabled   bool   `json:"telemetry_core_enabled"`
		TelemetryUserEnabled   bool   `json:"telemetry_user_enabled"`
		TelemetryMELevel       string `json:"telemetry_me_level"`
	}{}
	if err := c.getJSON(ctx, "/v1/security/posture", &posture); err != nil {
		return RuntimeState{}, err
	}

	gates := struct {
		AcceptingNewConnections bool    `json:"accepting_new_connections"`
		MERuntimeReady          bool    `json:"me_runtime_ready"`
		ME2DCFallbackEnabled    bool    `json:"me2dc_fallback_enabled"`
		UseMiddleProxy          bool    `json:"use_middle_proxy"`
		StartupStatus           string  `json:"startup_status"`
		StartupStage            string  `json:"startup_stage"`
		StartupProgressPct      float64 `json:"startup_progress_pct"`
	}{}
	if err := c.getJSON(ctx, "/v1/runtime/gates", &gates); err != nil {
		return RuntimeState{}, err
	}

	initialization := struct {
		Status        string  `json:"status"`
		Degraded      bool    `json:"degraded"`
		CurrentStage  string  `json:"current_stage"`
		ProgressPct   float64 `json:"progress_pct"`
		TransportMode string  `json:"transport_mode"`
	}{}
	if err := c.getJSON(ctx, "/v1/runtime/initialization", &initialization); err != nil {
		return RuntimeState{}, err
	}

	connectionSummary := struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
		Data    struct {
			Totals struct {
				CurrentConnections       int `json:"current_connections"`
				CurrentConnectionsME     int `json:"current_connections_me"`
				CurrentConnectionsDirect int `json:"current_connections_direct"`
				ActiveUsers              int `json:"active_users"`
			} `json:"totals"`
		} `json:"data"`
	}{}
	if err := c.getJSON(ctx, "/v1/runtime/connections/summary", &connectionSummary); err != nil {
		return RuntimeState{}, err
	}

	summary := struct {
		ConnectionsTotal       uint64 `json:"connections_total"`
		ConnectionsBadTotal    uint64 `json:"connections_bad_total"`
		HandshakeTimeoutsTotal uint64 `json:"handshake_timeouts_total"`
		ConfiguredUsers        int    `json:"configured_users"`
	}{}
	if err := c.getJSON(ctx, "/v1/stats/summary", &summary); err != nil {
		return RuntimeState{}, err
	}

	dcStatus := struct {
		DCS []struct {
			DC                 int     `json:"dc"`
			AvailableEndpoints int     `json:"available_endpoints"`
			AvailablePct       float64 `json:"available_pct"`
			RequiredWriters    int     `json:"required_writers"`
			AliveWriters       int     `json:"alive_writers"`
			CoveragePct        float64 `json:"coverage_pct"`
			RTTMs              float64 `json:"rtt_ms"`
			Load               int     `json:"load"`
		} `json:"dcs"`
	}{}
	if err := c.getJSON(ctx, "/v1/stats/dcs", &dcStatus); err != nil {
		return RuntimeState{}, err
	}

	dcs := make([]RuntimeDC, 0, len(dcStatus.DCS))
	for _, dc := range dcStatus.DCS {
		dcs = append(dcs, RuntimeDC{
			DC:                 dc.DC,
			AvailableEndpoints: dc.AvailableEndpoints,
			AvailablePct:       dc.AvailablePct,
			RequiredWriters:    dc.RequiredWriters,
			AliveWriters:       dc.AliveWriters,
			CoveragePct:        dc.CoveragePct,
			RTTMs:              dc.RTTMs,
			Load:               dc.Load,
		})
	}

	now := time.Now().UTC()
	slowData := slowRuntimeState{}
	useCachedSlowData := false
	if c.slowDataTTL > 0 {
		c.mu.RLock()
		if c.hasSlowData && now.Sub(c.slowFetchedAt) < c.slowDataTTL {
			slowData = c.slowData
			useCachedSlowData = true
		}
		c.mu.RUnlock()
	}
	if !useCachedSlowData {
		fetchedSlowData, err := c.fetchSlowRuntimeState(ctx)
		if err != nil {
			return RuntimeState{}, err
		}
		slowData = fetchedSlowData
		if c.slowDataTTL > 0 {
			c.mu.Lock()
			c.slowData = fetchedSlowData
			c.slowFetchedAt = now
			c.hasSlowData = true
			c.mu.Unlock()
		}
	}

	users, err := c.fetchClientUsage(ctx)
	if err != nil {
		return RuntimeState{}, err
	}

	return RuntimeState{
		Version:        slowData.Version,
		ReadOnly:       posture.ReadOnly || posture.APIReadOnly,
		UptimeSeconds:  slowData.UptimeSeconds,
		ConnectedUsers: connectionSummary.Data.Totals.CurrentConnections,
		Gates: RuntimeGates{
			AcceptingNewConnections: gates.AcceptingNewConnections,
			MERuntimeReady:          gates.MERuntimeReady,
			ME2DCFallbackEnabled:    gates.ME2DCFallbackEnabled,
			UseMiddleProxy:          gates.UseMiddleProxy,
			StartupStatus:           gates.StartupStatus,
			StartupStage:            gates.StartupStage,
			StartupProgressPct:      gates.StartupProgressPct,
		},
		Initialization: RuntimeInitialization{
			Status:        initialization.Status,
			Degraded:      initialization.Degraded,
			CurrentStage:  initialization.CurrentStage,
			ProgressPct:   initialization.ProgressPct,
			TransportMode: initialization.TransportMode,
		},
		ConnectionTotals: RuntimeConnectionTotals{
			CurrentConnections:       connectionSummary.Data.Totals.CurrentConnections,
			CurrentConnectionsME:     connectionSummary.Data.Totals.CurrentConnectionsME,
			CurrentConnectionsDirect: connectionSummary.Data.Totals.CurrentConnectionsDirect,
			ActiveUsers:              connectionSummary.Data.Totals.ActiveUsers,
		},
		Summary: RuntimeSummary{
			ConnectionsTotal:       summary.ConnectionsTotal,
			ConnectionsBadTotal:    summary.ConnectionsBadTotal,
			HandshakeTimeoutsTotal: summary.HandshakeTimeoutsTotal,
			ConfiguredUsers:        summary.ConfiguredUsers,
		},
		DCs:          dcs,
		Upstreams:    slowData.Upstreams,
		RecentEvents: slowData.RecentEvents,
		Diagnostics:  slowData.Diagnostics,
		SecurityInventory: slowData.SecurityInventory,
		Clients:      users,
	}, nil
}

// fetchSlowRuntimeState reads the heavier Telemt endpoints that do not need live refresh on every snapshot.
func (c *Client) fetchSlowRuntimeState(ctx context.Context) (slowRuntimeState, error) {
	systemInfo, err := c.getJSONPayload(ctx, "/v1/system/info")
	if err != nil {
		return slowRuntimeState{}, err
	}

	upstreamStatus := struct {
		Summary struct {
			ConfiguredTotal int `json:"configured_total"`
			HealthyTotal    int `json:"healthy_total"`
			UnhealthyTotal  int `json:"unhealthy_total"`
			DirectTotal     int `json:"direct_total"`
			SOCKS5Total     int `json:"socks5_total"`
		} `json:"summary"`
		Upstreams []struct {
			UpstreamID         int     `json:"upstream_id"`
			RouteKind          string  `json:"route_kind"`
			Address            string  `json:"address"`
			Healthy            bool    `json:"healthy"`
			Fails              int     `json:"fails"`
			EffectiveLatencyMs float64 `json:"effective_latency_ms"`
		} `json:"upstreams"`
	}{}
	if err := c.getJSON(ctx, "/v1/stats/upstreams", &upstreamStatus); err != nil {
		return slowRuntimeState{}, err
	}

	recentEvents := struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
		Data    struct {
			Events []struct {
				Sequence      uint64 `json:"seq"`
				TimestampUnix int64  `json:"ts_epoch_secs"`
				EventType     string `json:"event_type"`
				Context       string `json:"context"`
			} `json:"events"`
		} `json:"data"`
	}{}
	// Recent events are advisory diagnostics. A temporary read failure must not suppress
	// the core operator snapshot built from health, gates, connections, summary, and DC state.
	if err := c.getJSON(ctx, "/v1/runtime/events/recent", &recentEvents); err != nil {
		recentEvents = struct {
			Enabled bool   `json:"enabled"`
			Reason  string `json:"reason"`
			Data    struct {
				Events []struct {
					Sequence      uint64 `json:"seq"`
					TimestampUnix int64  `json:"ts_epoch_secs"`
					EventType     string `json:"event_type"`
					Context       string `json:"context"`
				} `json:"events"`
			} `json:"data"`
		}{}
	}

	diagnostics := RuntimeDiagnostics{
		State:       "fresh",
		StateReason: "",
		SystemInfoJSON: marshalRawJSON(systemInfo),
	}

	if payload, err := c.getJSONPayload(ctx, "/v1/limits/effective"); err == nil {
		diagnostics.EffectiveLimitsJSON = marshalRawJSON(payload)
	} else {
		diagnostics.State = "unavailable"
		diagnostics.StateReason = "limits_unavailable"
	}

	if payload, err := c.getJSONPayload(ctx, "/v1/security/posture"); err == nil {
		diagnostics.SecurityPostureJSON = marshalRawJSON(payload)
	} else if diagnostics.State == "fresh" {
		diagnostics.State = "unavailable"
		diagnostics.StateReason = "posture_unavailable"
	}

	minimalAll := map[string]any{}
	if err := c.getJSON(ctx, "/v1/stats/minimal/all", &minimalAll); err == nil {
		diagnostics.MinimalAllJSON = marshalJSON(minimalAll)
	} else if diagnostics.State == "fresh" {
		diagnostics.State = "unavailable"
		diagnostics.StateReason = "minimal_runtime_unavailable"
	}

	mePool := map[string]any{}
	if err := c.getJSON(ctx, "/v1/runtime/me_pool_state", &mePool); err == nil {
		diagnostics.MEPoolJSON = marshalJSON(mePool)
	} else if diagnostics.State == "fresh" {
		diagnostics.State = "unavailable"
		diagnostics.StateReason = "me_pool_unavailable"
	}

	securityInventory := RuntimeSecurityInventory{
		State:       "fresh",
		StateReason: "",
	}
	whitelist := struct {
		Enabled     bool     `json:"enabled"`
		EntriesTotal int     `json:"entries_total"`
		Entries     []string `json:"entries"`
	}{}
	if err := c.getJSON(ctx, "/v1/security/whitelist", &whitelist); err == nil {
		securityInventory.Enabled = whitelist.Enabled
		securityInventory.EntriesTotal = whitelist.EntriesTotal
		securityInventory.EntriesJSON = marshalJSON(whitelist.Entries)
	} else {
		securityInventory.State = "unavailable"
		securityInventory.StateReason = "whitelist_unavailable"
	}

	upstreams := make([]RuntimeUpstream, 0, len(upstreamStatus.Upstreams))
	for _, upstream := range upstreamStatus.Upstreams {
		upstreams = append(upstreams, RuntimeUpstream{
			UpstreamID:         upstream.UpstreamID,
			RouteKind:          upstream.RouteKind,
			Address:            upstream.Address,
			Healthy:            upstream.Healthy,
			Fails:              upstream.Fails,
			EffectiveLatencyMs: upstream.EffectiveLatencyMs,
		})
	}

	events := make([]RuntimeEvent, 0, len(recentEvents.Data.Events))
	for _, event := range recentEvents.Data.Events {
		events = append(events, RuntimeEvent{
			Sequence:      event.Sequence,
			TimestampUnix: event.TimestampUnix,
			EventType:     event.EventType,
			Context:       event.Context,
		})
	}

	return slowRuntimeState{
		Version:       jsonString(systemInfo["version"]),
		UptimeSeconds: jsonFloat(systemInfo["uptime_seconds"]),
		Upstreams: RuntimeUpstreamSummary{
			ConfiguredTotal: upstreamStatus.Summary.ConfiguredTotal,
			HealthyTotal:    upstreamStatus.Summary.HealthyTotal,
			UnhealthyTotal:  upstreamStatus.Summary.UnhealthyTotal,
			DirectTotal:     upstreamStatus.Summary.DirectTotal,
			SOCKS5Total:     upstreamStatus.Summary.SOCKS5Total,
			Rows:            upstreams,
		},
		RecentEvents: events,
		Diagnostics: diagnostics,
		SecurityInventory: securityInventory,
	}, nil
}

func (c *Client) getJSONPayload(ctx context.Context, path string) (map[string]any, error) {
	payload := make(map[string]any)
	if err := c.getJSON(ctx, path, &payload); err != nil {
		return nil, err
	}

	return payload, nil
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
	result := make([]ClientUsage, 0, len(parsed.Users))
	for username, m := range parsed.Users {
		result = append(result, ClientUsage{
			ClientName:       username,
			TrafficUsedBytes: m.OctetsFromClient + m.OctetsToClient,
			ActiveTCPConns:   m.CurrentConnections,
			CurrentIPsUsed:   m.UniqueIPsCurrent,
		})
	}

	return ClientUsageMetricsSnapshot{
		Users:         result,
		UptimeSeconds: parsed.UptimeSeconds,
	}, nil
}

// FetchActiveIPs fetches the /v1/stats/users/active-ips endpoint and returns per-user active IPs.
func (c *Client) FetchActiveIPs(ctx context.Context) ([]UserActiveIPs, error) {
	var users []UserActiveIPs
	if err := c.getJSON(ctx, "/v1/stats/users/active-ips", &users); err != nil {
		return nil, err
	}

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
		"username":         client.Name,
		"secret":           client.Secret,
		"user_ad_tag":      client.UserADTag,
		"enabled":          client.Enabled,
		"max_tcp_conns":    client.MaxTCPConns,
		"max_unique_ips":   client.MaxUniqueIPs,
		"data_quota_bytes": client.DataQuotaBytes,
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

	var body struct {
		Links struct {
			TLS     []string `json:"tls"`
			Secure  []string `json:"secure"`
			Classic []string `json:"classic"`
		} `json:"links"`
	}
	if err := decodeSuccessData(response.Body, &body); err != nil {
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
		return fmt.Errorf("telemt request failed: %w", decodeAPIError(response.Body, fmt.Sprintf("telemt request failed with status %d", response.StatusCode)))
	}

	return decodeSuccessData(response.Body, dest)
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

const maxResponseBodySize = 10 << 20 // 10 MiB

func decodeSuccessData(body io.Reader, dest any) error {
	payload, err := io.ReadAll(io.LimitReader(body, maxResponseBodySize))
	if err != nil {
		return err
	}

	var envelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil && len(envelope.Data) > 0 {
		return json.Unmarshal(envelope.Data, dest)
	}

	return json.Unmarshal(payload, dest)
}

func decodeAPIError(body io.Reader, fallback string) error {
	payload, err := io.ReadAll(io.LimitReader(body, maxResponseBodySize))
	if err != nil {
		return err
	}

	var envelope struct {
		OK      bool            `json:"ok"`
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil {
		code, message := decodeAPIErrorDetails(envelope.Error)
		if message == "" {
			message = strings.TrimSpace(envelope.Message)
		}

		switch {
		case code != "" && message != "":
			return fmt.Errorf("%s: %s", code, message)
		case code != "":
			return errors.New(code)
		case message != "":
			return fmt.Errorf("%s: %s", fallback, message)
		}
	}

	trimmed := strings.Join(strings.Fields(string(payload)), " ")
	if trimmed != "" {
		return fmt.Errorf("%s: %s", fallback, trimmed)
	}

	return errors.New(fallback)
}

func decodeAPIErrorDetails(raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}

	var code string
	if err := json.Unmarshal(raw, &code); err == nil {
		return strings.TrimSpace(code), ""
	}

	var details struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &details); err == nil {
		return strings.TrimSpace(details.Code), strings.TrimSpace(details.Message)
	}

	return "", ""
}

func marshalJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}

	return string(data)
}

func marshalRawJSON(value map[string]any) string {
	return marshalJSON(value)
}

func jsonString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func jsonFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint64:
		return float64(typed)
	default:
		return 0
	}
}
