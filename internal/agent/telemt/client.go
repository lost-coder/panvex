package telemt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

// defaultFetchRuntimeStateDeadline bounds the total duration of a FetchRuntimeState
// cycle when the caller supplies a context without its own deadline. Without this
// cap, a hung Telemt subsystem could block the snapshot loop for up to
// len(subfetches) * defaultRequestTimeout (~150s) on each cycle. See P2-REL-07.
const defaultFetchRuntimeStateDeadline = 30 * time.Second

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
	logger        *slog.Logger
	systemLoadSampler func(context.Context) (RuntimeSystemLoad, error)
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
	Version           string
	UptimeSeconds     float64
	Upstreams         RuntimeUpstreamSummary
	RecentEvents      []RuntimeEvent
	Diagnostics       RuntimeDiagnostics
	SecurityInventory RuntimeSecurityInventory
	MeWritersSummary  RuntimeMeWritersSummary
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
	MeWritersSummary  RuntimeMeWritersSummary
	SystemLoad       RuntimeSystemLoad
	Clients          []ClientUsage
	// Partial indicates that at least one Telemt sub-fetch failed or the
	// outer context expired during FetchRuntimeState. Downstream callers
	// should log a warning and may still forward the snapshot to the
	// control-plane; absent sub-fields fall back to zero values. See P2-REL-07.
	Partial bool
}

// RuntimeSystemLoad carries short server load telemetry for trend history charts.
type RuntimeSystemLoad struct {
	CPUUsagePct      float64
	MemoryUsedBytes  uint64
	MemoryTotalBytes uint64
	MemoryUsagePct   float64
	DiskUsedBytes    uint64
	DiskTotalBytes   uint64
	DiskUsagePct     float64
	Load1M           float64
	Load5M           float64
	Load15M          float64
	NetBytesSent     uint64
	NetBytesRecv     uint64
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
	DcsJSON             string
}

// RuntimeSecurityInventory carries whitelist inventory data used by security detail sections.
type RuntimeSecurityInventory struct {
	State        string
	StateReason  string
	Enabled      bool
	EntriesTotal int
	EntriesJSON  string
}

// RuntimeMeWritersSummary carries the ME writers pool aggregate from /v1/stats/me-writers.
type RuntimeMeWritersSummary struct {
	ConfiguredEndpoints int
	AvailableEndpoints  int
	CoveragePct         float64
	FreshAliveWriters   int
	FreshCoveragePct    float64
	RequiredWriters     int
	AliveWriters        int
}

// RuntimeGates carries the operator-facing admission and transport gates.
type RuntimeGates struct {
	AcceptingNewConnections bool
	MERuntimeReady          bool
	ME2DCFallbackEnabled    bool
	ME2DCFastEnabled        bool
	UseMiddleProxy          bool
	RouteMode               string
	RerouteActive           bool
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
// RuntimeConnectionTopEntry carries one entry from the top-N connections or throughput list.
type RuntimeConnectionTopEntry struct {
	Username        string
	Connections     int
	ThroughputBytes uint64
}

type RuntimeConnectionTotals struct {
	CurrentConnections       int
	CurrentConnectionsME     int
	CurrentConnectionsDirect int
	ActiveUsers              int
	StaleCacheUsed           bool
	TopByConnections         []RuntimeConnectionTopEntry
	TopByThroughput          []RuntimeConnectionTopEntry
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
	FreshAliveWriters  int
	FreshCoveragePct   float64
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
	Weight             int
	LastCheckAgeSecs   int
	Scopes             []string
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
		logger:        slog.Default(),
		systemLoadSampler: collectLocalSystemLoad,
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
//
// When the caller's context has no deadline, FetchRuntimeState installs an
// internal defaultFetchRuntimeStateDeadline (30s) so a hung Telemt subsystem
// cannot stall the snapshot loop for the cumulative sum of the ten per-request
// http.Client timeouts (~150s). Callers that already have their own deadline
// (for example the supervisor loop) are respected as-is. See P2-REL-07.
//
// Partial-snapshot semantics: if any sub-fetch fails or ctx expires mid-cycle,
// FetchRuntimeState returns what it managed to collect with Partial=true and
// a nil error. Missing sub-fields fall back to zero values. This lets the
// agent keep heartbeating degraded state to the control-plane instead of
// dropping whole cycles whenever one endpoint is slow.
func (c *Client) FetchRuntimeState(ctx context.Context) (RuntimeState, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultFetchRuntimeStateDeadline)
		defer cancel()
	}

	result := RuntimeState{}
	markPartial := func(path string, err error) {
		result.Partial = true
		c.logger.Warn("telemt runtime sub-fetch failed",
			"path", path,
			"err", err,
			"ctx_err", ctx.Err(),
		)
	}

	health := struct {
		Status string `json:"status"`
	}{}
	if err := c.getJSON(ctx, "/v1/health", &health); err != nil {
		markPartial("/v1/health", err)
	} else {
		c.logger.Debug("telemt api call", "path", "/v1/health", "status", health.Status)
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
		markPartial("/v1/security/posture", err)
	}

	gates := struct {
		AcceptingNewConnections bool    `json:"accepting_new_connections"`
		MERuntimeReady          bool    `json:"me_runtime_ready"`
		ME2DCFallbackEnabled    bool    `json:"me2dc_fallback_enabled"`
		ME2DCFastEnabled        bool    `json:"me2dc_fast_enabled"`
		UseMiddleProxy          bool    `json:"use_middle_proxy"`
		RouteMode               string  `json:"route_mode"`
		RerouteActive           bool    `json:"reroute_active"`
		StartupStatus           string  `json:"startup_status"`
		StartupStage            string  `json:"startup_stage"`
		StartupProgressPct      float64 `json:"startup_progress_pct"`
	}{}
	if err := c.getJSON(ctx, "/v1/runtime/gates", &gates); err != nil {
		markPartial("/v1/runtime/gates", err)
	} else {
		c.logger.Debug("telemt api call", "path", "/v1/runtime/gates", "accepting", gates.AcceptingNewConnections)
	}

	initialization := struct {
		Status        string  `json:"status"`
		Degraded      bool    `json:"degraded"`
		CurrentStage  string  `json:"current_stage"`
		ProgressPct   float64 `json:"progress_pct"`
		TransportMode string  `json:"transport_mode"`
	}{}
	if err := c.getJSON(ctx, "/v1/runtime/initialization", &initialization); err != nil {
		markPartial("/v1/runtime/initialization", err)
	}

	connectionSummary := struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
		Data    struct {
			Cache struct {
				StaleCacheUsed bool `json:"stale_cache_used"`
			} `json:"cache"`
			Totals struct {
				CurrentConnections       int `json:"current_connections"`
				CurrentConnectionsME     int `json:"current_connections_me"`
				CurrentConnectionsDirect int `json:"current_connections_direct"`
				ActiveUsers              int `json:"active_users"`
			} `json:"totals"`
			Top struct {
				ByConnections []struct {
					Username    string `json:"username"`
					Connections int    `json:"connections"`
				} `json:"by_connections"`
				ByThroughput []struct {
					Username       string `json:"username"`
					ThroughputBytes uint64 `json:"throughput_bytes"`
				} `json:"by_throughput"`
			} `json:"top"`
		} `json:"data"`
	}{}
	if err := c.getJSON(ctx, "/v1/runtime/connections/summary", &connectionSummary); err != nil {
		markPartial("/v1/runtime/connections/summary", err)
	} else {
		c.logger.Debug("telemt api call", "path", "/v1/runtime/connections/summary", "connections", connectionSummary.Data.Totals.CurrentConnections)
	}

	summary := struct {
		ConnectionsTotal       uint64 `json:"connections_total"`
		ConnectionsBadTotal    uint64 `json:"connections_bad_total"`
		HandshakeTimeoutsTotal uint64 `json:"handshake_timeouts_total"`
		ConfiguredUsers        int    `json:"configured_users"`
	}{}
	if err := c.getJSON(ctx, "/v1/stats/summary", &summary); err != nil {
		markPartial("/v1/stats/summary", err)
	}

	dcStatus := struct {
		DCS []struct {
			DC                 int     `json:"dc"`
			AvailableEndpoints int     `json:"available_endpoints"`
			AvailablePct       float64 `json:"available_pct"`
			RequiredWriters    int     `json:"required_writers"`
			AliveWriters       int     `json:"alive_writers"`
			CoveragePct        float64 `json:"coverage_pct"`
			FreshAliveWriters  int     `json:"fresh_alive_writers"`
			FreshCoveragePct   float64 `json:"fresh_coverage_pct"`
			RTTMs              float64 `json:"rtt_ms"`
			Load               int     `json:"load"`
		} `json:"dcs"`
	}{}
	if err := c.getJSON(ctx, "/v1/stats/dcs", &dcStatus); err != nil {
		markPartial("/v1/stats/dcs", err)
	} else {
		c.logger.Debug("telemt api call", "path", "/v1/stats/dcs", "dc_count", len(dcStatus.DCS))
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
			FreshAliveWriters:  dc.FreshAliveWriters,
			FreshCoveragePct:   dc.FreshCoveragePct,
			RTTMs:              dc.RTTMs,
			Load:               dc.Load,
		})
	}

	slowData := slowRuntimeState{}
	useCachedSlowData := false
	if c.slowDataTTL > 0 {
		c.mu.RLock()
		now := time.Now().UTC()
		if c.hasSlowData && now.Sub(c.slowFetchedAt) < c.slowDataTTL {
			slowData = c.slowData
			useCachedSlowData = true
		}
		c.mu.RUnlock()
	}
	if !useCachedSlowData {
		fetchedSlowData, slowPartial, err := c.fetchSlowRuntimeState(ctx)
		if err != nil {
			markPartial("slow_runtime_state", err)
		} else {
			slowData = fetchedSlowData
			if slowPartial {
				result.Partial = true
			}
			// Only cache when we actually obtained a usable slow snapshot; if the
			// slow bundle itself reported internal degradation we still cache the
			// payload because its sub-sections carry their own "state" markers.
			if c.slowDataTTL > 0 {
				c.mu.Lock()
				c.slowData = fetchedSlowData
				c.slowFetchedAt = time.Now().UTC()
				c.hasSlowData = true
				c.mu.Unlock()
			}
		}
	}

	users, err := c.fetchClientUsage(ctx)
	if err != nil {
		markPartial("/v1/stats/users", err)
		users = nil
	}
	systemLoad := RuntimeSystemLoad{}
	if c.systemLoadSampler != nil {
		if load, err := c.systemLoadSampler(ctx); err == nil {
			systemLoad = load
		} else {
			markPartial("system_load_sampler", err)
		}
	}

	partial := result.Partial
	result = RuntimeState{
		Version:        slowData.Version,
		ReadOnly:       posture.ReadOnly || posture.APIReadOnly,
		UptimeSeconds:  slowData.UptimeSeconds,
		ConnectedUsers: connectionSummary.Data.Totals.CurrentConnections,
		Gates: RuntimeGates{
			AcceptingNewConnections: gates.AcceptingNewConnections,
			MERuntimeReady:          gates.MERuntimeReady,
			ME2DCFallbackEnabled:    gates.ME2DCFallbackEnabled,
			ME2DCFastEnabled:        gates.ME2DCFastEnabled,
			UseMiddleProxy:          gates.UseMiddleProxy,
			RouteMode:               gates.RouteMode,
			RerouteActive:           gates.RerouteActive,
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
		ConnectionTotals: func() RuntimeConnectionTotals {
			topConn := make([]RuntimeConnectionTopEntry, 0, len(connectionSummary.Data.Top.ByConnections))
			for _, e := range connectionSummary.Data.Top.ByConnections {
				topConn = append(topConn, RuntimeConnectionTopEntry{Username: e.Username, Connections: e.Connections})
			}
			topTput := make([]RuntimeConnectionTopEntry, 0, len(connectionSummary.Data.Top.ByThroughput))
			for _, e := range connectionSummary.Data.Top.ByThroughput {
				topTput = append(topTput, RuntimeConnectionTopEntry{Username: e.Username, ThroughputBytes: e.ThroughputBytes})
			}
			return RuntimeConnectionTotals{
				CurrentConnections:       connectionSummary.Data.Totals.CurrentConnections,
				CurrentConnectionsME:     connectionSummary.Data.Totals.CurrentConnectionsME,
				CurrentConnectionsDirect: connectionSummary.Data.Totals.CurrentConnectionsDirect,
				ActiveUsers:              connectionSummary.Data.Totals.ActiveUsers,
				StaleCacheUsed:           connectionSummary.Data.Cache.StaleCacheUsed,
				TopByConnections:         topConn,
				TopByThroughput:          topTput,
			}
		}(),
		Summary: RuntimeSummary{
			ConnectionsTotal:       summary.ConnectionsTotal,
			ConnectionsBadTotal:    summary.ConnectionsBadTotal,
			HandshakeTimeoutsTotal: summary.HandshakeTimeoutsTotal,
			ConfiguredUsers:        summary.ConfiguredUsers,
		},
		DCs:          dcs,
		Upstreams:    slowData.Upstreams,
		RecentEvents: slowData.RecentEvents,
		Diagnostics:      slowData.Diagnostics,
		SecurityInventory: slowData.SecurityInventory,
		MeWritersSummary: slowData.MeWritersSummary,
		SystemLoad:       systemLoad,
		Clients:      users,
		Partial:      partial,
	}
	return result, nil
}

// fetchSlowRuntimeState reads the heavier Telemt endpoints that do not need live refresh on every snapshot.
//
// Returns the collected slow state, a partial flag (true when at least one
// advisory sub-endpoint failed but the core system/info payload still arrived),
// and a non-nil error only when the required /v1/system/info payload itself is
// unreachable. See P2-REL-07.
func (c *Client) fetchSlowRuntimeState(ctx context.Context) (slowRuntimeState, bool, error) {
	systemInfo, err := c.getJSONPayload(ctx, "/v1/system/info")
	if err != nil {
		return slowRuntimeState{}, true, err
	}
	partial := false

	upstreamStatus := struct {
		Summary struct {
			ConfiguredTotal int `json:"configured_total"`
			HealthyTotal    int `json:"healthy_total"`
			UnhealthyTotal  int `json:"unhealthy_total"`
			DirectTotal     int `json:"direct_total"`
			SOCKS5Total     int `json:"socks5_total"`
		} `json:"summary"`
		Upstreams []struct {
			UpstreamID         int      `json:"upstream_id"`
			RouteKind          string   `json:"route_kind"`
			Address            string   `json:"address"`
			Healthy            bool     `json:"healthy"`
			Fails              int      `json:"fails"`
			EffectiveLatencyMs float64  `json:"effective_latency_ms"`
			Weight             int `json:"weight"`
			LastCheckAgeSecs   int `json:"last_check_age_secs"`
			Scopes             any `json:"scopes"`
		} `json:"upstreams"`
	}{}
	if err := c.getJSON(ctx, "/v1/stats/upstreams", &upstreamStatus); err != nil {
		partial = true
		c.logger.Warn("telemt runtime sub-fetch failed", "path", "/v1/stats/upstreams", "err", err, "ctx_err", ctx.Err())
	} else {
		c.logger.Debug("telemt api call", "path", "/v1/stats/upstreams", "configured", upstreamStatus.Summary.ConfiguredTotal)
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
		partial = true
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
		partial = true
		diagnostics.State = "unavailable"
		diagnostics.StateReason = "limits_unavailable"
	}

	if payload, err := c.getJSONPayload(ctx, "/v1/security/posture"); err == nil {
		diagnostics.SecurityPostureJSON = marshalRawJSON(payload)
	} else {
		partial = true
		if diagnostics.State == "fresh" {
			diagnostics.State = "unavailable"
			diagnostics.StateReason = "posture_unavailable"
		}
	}

	minimalAll := map[string]any{}
	if err := c.getJSON(ctx, "/v1/stats/minimal/all", &minimalAll); err == nil {
		diagnostics.MinimalAllJSON = marshalJSON(minimalAll)
	} else {
		partial = true
		if diagnostics.State == "fresh" {
			diagnostics.State = "unavailable"
			diagnostics.StateReason = "minimal_runtime_unavailable"
		}
	}

	mePool := map[string]any{}
	if err := c.getJSON(ctx, "/v1/runtime/me_pool_state", &mePool); err == nil {
		c.logger.Debug("telemt api call", "path", "/v1/runtime/me_pool_state")
		diagnostics.MEPoolJSON = marshalJSON(mePool)
	} else {
		partial = true
		if diagnostics.State == "fresh" {
			diagnostics.State = "unavailable"
			diagnostics.StateReason = "me_pool_unavailable"
		}
	}

	dcsDetail := map[string]any{}
	if err := c.getJSON(ctx, "/v1/stats/dcs", &dcsDetail); err == nil {
		diagnostics.DcsJSON = marshalJSON(dcsDetail)
	} else {
		partial = true
	}

	meWritersSummary := RuntimeMeWritersSummary{}
	meWritersResp := struct {
		Summary struct {
			ConfiguredEndpoints int     `json:"configured_endpoints"`
			AvailableEndpoints  int     `json:"available_endpoints"`
			CoveragePct         float64 `json:"coverage_pct"`
			FreshAliveWriters   int     `json:"fresh_alive_writers"`
			FreshCoveragePct    float64 `json:"fresh_coverage_pct"`
			RequiredWriters     int     `json:"required_writers"`
			AliveWriters        int     `json:"alive_writers"`
		} `json:"summary"`
	}{}
	if err := c.getJSON(ctx, "/v1/stats/me-writers", &meWritersResp); err != nil {
		partial = true
	} else {
		c.logger.Debug("telemt api call", "path", "/v1/stats/me-writers", "alive_writers", meWritersResp.Summary.AliveWriters)
		meWritersSummary = RuntimeMeWritersSummary{
			ConfiguredEndpoints: meWritersResp.Summary.ConfiguredEndpoints,
			AvailableEndpoints:  meWritersResp.Summary.AvailableEndpoints,
			CoveragePct:         meWritersResp.Summary.CoveragePct,
			FreshAliveWriters:   meWritersResp.Summary.FreshAliveWriters,
			FreshCoveragePct:    meWritersResp.Summary.FreshCoveragePct,
			RequiredWriters:     meWritersResp.Summary.RequiredWriters,
			AliveWriters:        meWritersResp.Summary.AliveWriters,
		}
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
		partial = true
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
			Weight:             upstream.Weight,
			LastCheckAgeSecs:   upstream.LastCheckAgeSecs,
			Scopes:             parseScopes(upstream.Scopes),
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
		RecentEvents:     events,
		Diagnostics:      diagnostics,
		SecurityInventory: securityInventory,
		MeWritersSummary: meWritersSummary,
	}, partial, nil
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
	c.logger.Debug("telemt api call", "path", "/metrics", "user_count", len(parsed.Users))
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
	c.logger.Debug("telemt api call", "path", "/v1/stats/users/active-ips", "user_count", len(users))

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

// parseScopes normalizes the Telemt scopes field which may be a single string or an array of strings.
func parseScopes(v any) []string {
	switch val := v.(type) {
	case string:
		if val == "" {
			return nil
		}
		return []string{val}
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
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
