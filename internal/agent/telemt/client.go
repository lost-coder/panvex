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

// Telemt API endpoint paths and shared log keys. Centralised so the
// per-call sites read as a single token and Sonar S1192 stops firing on
// the duplicates.
const (
	pathHealth                    = "/v1/health"
	pathSecurityPosture           = "/v1/security/posture"
	pathRuntimeGates              = "/v1/runtime/gates"
	pathRuntimeConnectionsSummary = "/v1/runtime/connections/summary"
	pathStatsDcs                  = "/v1/stats/dcs"
	pathStatsUpstreams            = "/v1/stats/upstreams"

	logTelemtAPICall = "telemt api call"
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
	baseURL           *url.URL
	metricsURL        *url.URL
	authorization     string
	httpClient        *http.Client
	logger            *slog.Logger
	systemLoadSampler func(context.Context) (RuntimeSystemLoad, error)
	mu                sync.RWMutex
	slowDataTTL       time.Duration
	slowFetchedAt     time.Time
	slowData          slowRuntimeState
	hasSlowData       bool
}

// InvalidateSlowDataCache forces the next runtime snapshot to refetch slow diagnostics.
func (c *Client) InvalidateSlowDataCache() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.hasSlowData = false
	c.slowFetchedAt = time.Time{}
	c.slowData = slowRuntimeState{}
}

// connectionSummaryTop carries the top-N connection / throughput rows
// from /v1/runtime/connections/summary. Pulled out of the surrounding
// anonymous struct so the deeply-nested decode tree stays readable
// (S8205).
type connectionSummaryTop struct {
	ByConnections []struct {
		Username    string `json:"username"`
		Connections int    `json:"connections"`
	} `json:"by_connections"`
	ByThroughput []struct {
		Username        string `json:"username"`
		ThroughputBytes uint64 `json:"throughput_bytes"`
	} `json:"by_throughput"`
}

// connectionSummaryData is the inner "data" payload of
// /v1/runtime/connections/summary. Extracted from the response decode
// struct to avoid an anonymous-struct nest (S8205).
type connectionSummaryData struct {
	Cache struct {
		StaleCacheUsed bool `json:"stale_cache_used"`
	} `json:"cache"`
	Totals struct {
		CurrentConnections       int `json:"current_connections"`
		CurrentConnectionsME     int `json:"current_connections_me"`
		CurrentConnectionsDirect int `json:"current_connections_direct"`
		ActiveUsers              int `json:"active_users"`
	} `json:"totals"`
	Top connectionSummaryTop `json:"top"`
}

// recentEventEntry is one row inside /v1/runtime/events/recent. Hoisted
// out of the response decode struct for readability (S8205).
type recentEventEntry struct {
	Sequence      uint64 `json:"seq"`
	TimestampUnix int64  `json:"ts_epoch_secs"`
	EventType     string `json:"event_type"`
	Context       string `json:"context"`
}

// recentEventsData is the inner "data" payload of
// /v1/runtime/events/recent. Extracted from the response decode struct
// for readability and so the on-failure reset can reuse the same type
// (S8205).
type recentEventsData struct {
	Events []recentEventEntry `json:"events"`
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
	Version           string
	ReadOnly          bool
	UptimeSeconds     float64
	ConnectedUsers    int
	Gates             RuntimeGates
	Initialization    RuntimeInitialization
	ConnectionTotals  RuntimeConnectionTotals
	Summary           RuntimeSummary
	DCs               []RuntimeDC
	Upstreams         RuntimeUpstreamSummary
	RecentEvents      []RuntimeEvent
	Diagnostics       RuntimeDiagnostics
	SecurityInventory RuntimeSecurityInventory
	MeWritersSummary  RuntimeMeWritersSummary
	SystemLoad        RuntimeSystemLoad
	Clients           []ClientUsage
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

// ClientApplyResult stores the link material returned after Telemt
// applies a client. Telemt's tls_domains config emits one TLS link per
// domain (×host), plus optional Secure/Classic alternates; we forward
// every non-empty entry so the panel can show all of them.
type ClientApplyResult struct {
	ConnectionLinks []string
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
		baseURL:           parsed,
		metricsURL:        metricsURL,
		authorization:     config.Authorization,
		httpClient:        httpClient,
		logger:            slog.Default(),
		systemLoadSampler: collectLocalSystemLoad,
		slowDataTTL:       defaultSlowDataTTL,
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
// fetchRuntimeStateRaw bundles the per-endpoint payloads collected during
// FetchRuntimeState so the assembly step can remain a straight-line
// projection without re-declaring the inner struct types.
type fetchRuntimeStateRaw struct {
	posture struct {
		ReadOnly             bool   `json:"read_only"`
		APIReadOnly          bool   `json:"api_read_only"`
		APIWhitelistEnabled  bool   `json:"api_whitelist_enabled"`
		APIWhitelistEntries  int    `json:"api_whitelist_entries"`
		APIAuthHeaderEnabled bool   `json:"api_auth_header_enabled"`
		ProxyProtocolEnabled bool   `json:"proxy_protocol_enabled"`
		LogLevel             string `json:"log_level"`
		TelemetryCoreEnabled bool   `json:"telemetry_core_enabled"`
		TelemetryUserEnabled bool   `json:"telemetry_user_enabled"`
		TelemetryMELevel     string `json:"telemetry_me_level"`
	}
	gates struct {
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
	}
	initialization struct {
		Status        string  `json:"status"`
		Degraded      bool    `json:"degraded"`
		CurrentStage  string  `json:"current_stage"`
		ProgressPct   float64 `json:"progress_pct"`
		TransportMode string  `json:"transport_mode"`
	}
	connectionSummary struct {
		Enabled bool                  `json:"enabled"`
		Reason  string                `json:"reason"`
		Data    connectionSummaryData `json:"data"`
	}
	summary struct {
		ConnectionsTotal       uint64 `json:"connections_total"`
		ConnectionsBadTotal    uint64 `json:"connections_bad_total"`
		HandshakeTimeoutsTotal uint64 `json:"handshake_timeouts_total"`
		ConfiguredUsers        int    `json:"configured_users"`
	}
	dcs        []RuntimeDC
	slowData   slowRuntimeState
	users      []ClientUsage
	systemLoad RuntimeSystemLoad
}

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
	setPartial := func() { result.Partial = true }

	raw := c.collectRuntimeStateRaw(ctx, markPartial, setPartial)

	partial := result.Partial
	return c.assembleRuntimeState(raw, partial), nil
}

// collectRuntimeStateRaw runs every Telemt sub-fetch FetchRuntimeState
// needs, marking partial-failures via markPartial (logs + flags) or
// setPartial (flag only). It is a thin orchestration wrapper so
// FetchRuntimeState's flow stays linear.
func (c *Client) collectRuntimeStateRaw(ctx context.Context, markPartial func(string, error), setPartial func()) fetchRuntimeStateRaw {
	raw := fetchRuntimeStateRaw{}

	c.fetchHealth(ctx, markPartial)
	c.fetchSecurityPosture(ctx, &raw, markPartial)
	c.fetchRuntimeGates(ctx, &raw, markPartial)
	c.fetchInitialization(ctx, &raw, markPartial)
	c.fetchConnectionSummary(ctx, &raw, markPartial)
	c.fetchSummary(ctx, &raw, markPartial)
	raw.dcs = c.fetchDCs(ctx, markPartial)

	c.collectSlowRuntimeState(ctx, &raw, markPartial, setPartial)

	users, err := c.fetchClientUsage(ctx)
	if err != nil {
		markPartial("/v1/stats/users", err)
		users = nil
	}
	raw.users = users

	if c.systemLoadSampler != nil {
		if load, err := c.systemLoadSampler(ctx); err == nil {
			raw.systemLoad = load
		} else {
			markPartial("system_load_sampler", err)
		}
	}
	return raw
}

// collectSlowRuntimeState fetches the cached slow snapshot, mirroring
// the original two-branch behaviour: a hard error produces a logged
// partial via markPartial; a slow-partial flag bumps Partial silently
// (the slow fetch already logged its own warnings).
func (c *Client) collectSlowRuntimeState(ctx context.Context, raw *fetchRuntimeStateRaw, markPartial func(string, error), setPartial func()) {
	slowData, slowPartial, slowErr := c.loadSlowRuntimeStateForFetch(ctx)
	if slowErr != nil {
		markPartial("slow_runtime_state", slowErr)
		return
	}
	raw.slowData = slowData
	if slowPartial {
		setPartial()
	}
}

func (c *Client) fetchHealth(ctx context.Context, markPartial func(string, error)) {
	health := struct {
		Status string `json:"status"`
	}{}
	if err := c.getJSON(ctx, pathHealth, &health); err != nil {
		markPartial(pathHealth, err)
		return
	}
	c.logger.Debug(logTelemtAPICall, "path", pathHealth, "status", health.Status)
}

func (c *Client) fetchSecurityPosture(ctx context.Context, raw *fetchRuntimeStateRaw, markPartial func(string, error)) {
	if err := c.getJSON(ctx, pathSecurityPosture, &raw.posture); err != nil {
		markPartial(pathSecurityPosture, err)
	}
}

func (c *Client) fetchRuntimeGates(ctx context.Context, raw *fetchRuntimeStateRaw, markPartial func(string, error)) {
	if err := c.getJSON(ctx, pathRuntimeGates, &raw.gates); err != nil {
		markPartial(pathRuntimeGates, err)
		return
	}
	c.logger.Debug(logTelemtAPICall, "path", pathRuntimeGates, "accepting", raw.gates.AcceptingNewConnections)
}

func (c *Client) fetchInitialization(ctx context.Context, raw *fetchRuntimeStateRaw, markPartial func(string, error)) {
	if err := c.getJSON(ctx, "/v1/runtime/initialization", &raw.initialization); err != nil {
		markPartial("/v1/runtime/initialization", err)
	}
}

func (c *Client) fetchConnectionSummary(ctx context.Context, raw *fetchRuntimeStateRaw, markPartial func(string, error)) {
	if err := c.getJSON(ctx, pathRuntimeConnectionsSummary, &raw.connectionSummary); err != nil {
		markPartial(pathRuntimeConnectionsSummary, err)
		return
	}
	c.logger.Debug(logTelemtAPICall, "path", pathRuntimeConnectionsSummary, "connections", raw.connectionSummary.Data.Totals.CurrentConnections)
}

func (c *Client) fetchSummary(ctx context.Context, raw *fetchRuntimeStateRaw, markPartial func(string, error)) {
	if err := c.getJSON(ctx, "/v1/stats/summary", &raw.summary); err != nil {
		markPartial("/v1/stats/summary", err)
	}
}

func (c *Client) fetchDCs(ctx context.Context, markPartial func(string, error)) []RuntimeDC {
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
	if err := c.getJSON(ctx, pathStatsDcs, &dcStatus); err != nil {
		markPartial(pathStatsDcs, err)
	} else {
		c.logger.Debug(logTelemtAPICall, "path", pathStatsDcs, "dc_count", len(dcStatus.DCS))
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
	return dcs
}

// assembleRuntimeState projects collected raw payloads into a
// RuntimeState. Pure transformation, no I/O.
func (c *Client) assembleRuntimeState(raw fetchRuntimeStateRaw, partial bool) RuntimeState {
	return RuntimeState{
		Version:        raw.slowData.Version,
		ReadOnly:       raw.posture.ReadOnly || raw.posture.APIReadOnly,
		UptimeSeconds:  raw.slowData.UptimeSeconds,
		ConnectedUsers: raw.connectionSummary.Data.Totals.CurrentConnections,
		Gates: RuntimeGates{
			AcceptingNewConnections: raw.gates.AcceptingNewConnections,
			MERuntimeReady:          raw.gates.MERuntimeReady,
			ME2DCFallbackEnabled:    raw.gates.ME2DCFallbackEnabled,
			ME2DCFastEnabled:        raw.gates.ME2DCFastEnabled,
			UseMiddleProxy:          raw.gates.UseMiddleProxy,
			RouteMode:               raw.gates.RouteMode,
			RerouteActive:           raw.gates.RerouteActive,
			StartupStatus:           raw.gates.StartupStatus,
			StartupStage:            raw.gates.StartupStage,
			StartupProgressPct:      raw.gates.StartupProgressPct,
		},
		Initialization: RuntimeInitialization{
			Status:        raw.initialization.Status,
			Degraded:      raw.initialization.Degraded,
			CurrentStage:  raw.initialization.CurrentStage,
			ProgressPct:   raw.initialization.ProgressPct,
			TransportMode: raw.initialization.TransportMode,
		},
		ConnectionTotals: buildConnectionTotalsFromSummary(raw.connectionSummary.Data),
		Summary: RuntimeSummary{
			ConnectionsTotal:       raw.summary.ConnectionsTotal,
			ConnectionsBadTotal:    raw.summary.ConnectionsBadTotal,
			HandshakeTimeoutsTotal: raw.summary.HandshakeTimeoutsTotal,
			ConfiguredUsers:        raw.summary.ConfiguredUsers,
		},
		DCs:               raw.dcs,
		Upstreams:         raw.slowData.Upstreams,
		RecentEvents:      raw.slowData.RecentEvents,
		Diagnostics:       raw.slowData.Diagnostics,
		SecurityInventory: raw.slowData.SecurityInventory,
		MeWritersSummary:  raw.slowData.MeWritersSummary,
		SystemLoad:        raw.systemLoad,
		Clients:           raw.users,
		Partial:           partial,
	}
}

// buildConnectionTotalsFromSummary converts the raw connection-summary
// payload into a RuntimeConnectionTotals.
func buildConnectionTotalsFromSummary(data connectionSummaryData) RuntimeConnectionTotals {
	topConn := make([]RuntimeConnectionTopEntry, 0, len(data.Top.ByConnections))
	for _, e := range data.Top.ByConnections {
		topConn = append(topConn, RuntimeConnectionTopEntry{Username: e.Username, Connections: e.Connections})
	}
	topTput := make([]RuntimeConnectionTopEntry, 0, len(data.Top.ByThroughput))
	for _, e := range data.Top.ByThroughput {
		topTput = append(topTput, RuntimeConnectionTopEntry{Username: e.Username, ThroughputBytes: e.ThroughputBytes})
	}
	return RuntimeConnectionTotals{
		CurrentConnections:       data.Totals.CurrentConnections,
		CurrentConnectionsME:     data.Totals.CurrentConnectionsME,
		CurrentConnectionsDirect: data.Totals.CurrentConnectionsDirect,
		ActiveUsers:              data.Totals.ActiveUsers,
		StaleCacheUsed:           data.Cache.StaleCacheUsed,
		TopByConnections:         topConn,
		TopByThroughput:          topTput,
	}
}

// fetchSlowRuntimeState reads the heavier Telemt endpoints that do not need live refresh on every snapshot.
//
// Returns the collected slow state, a partial flag (true when at least one
// advisory sub-endpoint failed but the core system/info payload still arrived),
// loadSlowRuntimeStateForFetch returns a slow-runtime snapshot: the cached
// copy if still fresh, otherwise a freshly-fetched payload that is also
// stored in the cache. The bool reports whether the slow snapshot itself
// was partial; the error is non-nil only when no usable snapshot could be
// obtained at all (cache miss + fetchSlowRuntimeState failure).
func (c *Client) loadSlowRuntimeStateForFetch(ctx context.Context) (slowRuntimeState, bool, error) {
	if c.slowDataTTL > 0 {
		c.mu.RLock()
		now := time.Now().UTC()
		cached := c.slowData
		fresh := c.hasSlowData && now.Sub(c.slowFetchedAt) < c.slowDataTTL
		c.mu.RUnlock()
		if fresh {
			return cached, false, nil
		}
	}

	fetched, slowPartial, err := c.fetchSlowRuntimeState(ctx)
	if err != nil {
		return slowRuntimeState{}, false, err
	}
	// Only cache when we actually obtained a usable slow snapshot; if the
	// slow bundle itself reported internal degradation we still cache the
	// payload because its sub-sections carry their own "state" markers.
	if c.slowDataTTL > 0 {
		c.mu.Lock()
		c.slowData = fetched
		c.slowFetchedAt = time.Now().UTC()
		c.hasSlowData = true
		c.mu.Unlock()
	}
	return fetched, slowPartial, nil
}

// and a non-nil error only when the required /v1/system/info payload itself is
// unreachable. See P2-REL-07.
// upstreamStatusResponse mirrors the /v1/stats/upstreams payload that
// fetchSlowRuntimeState consumes. Hoisted to package scope so helper
// functions can return it without redeclaring the anonymous struct.
type upstreamStatusResponse struct {
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
		Weight             int     `json:"weight"`
		LastCheckAgeSecs   int     `json:"last_check_age_secs"`
		Scopes             any     `json:"scopes"`
	} `json:"upstreams"`
}

type recentEventsResponse struct {
	Enabled bool             `json:"enabled"`
	Reason  string           `json:"reason"`
	Data    recentEventsData `json:"data"`
}

func (c *Client) fetchSlowRuntimeState(ctx context.Context) (slowRuntimeState, bool, error) {
	systemInfo, err := c.getJSONPayload(ctx, "/v1/system/info")
	if err != nil {
		return slowRuntimeState{}, true, err
	}
	partial := false

	upstreamStatus := c.fetchUpstreamStatus(ctx, &partial)
	recentEvents := c.fetchRecentEvents(ctx, &partial)
	diagnostics := c.buildSlowDiagnostics(ctx, systemInfo, &partial)
	meWritersSummary := c.fetchMeWritersSummary(ctx, &partial)
	securityInventory := c.fetchSecurityInventory(ctx, &partial)

	return slowRuntimeState{
		Version:       jsonString(systemInfo["version"]),
		UptimeSeconds: jsonFloat(systemInfo["uptime_seconds"]),
		Upstreams: RuntimeUpstreamSummary{
			ConfiguredTotal: upstreamStatus.Summary.ConfiguredTotal,
			HealthyTotal:    upstreamStatus.Summary.HealthyTotal,
			UnhealthyTotal:  upstreamStatus.Summary.UnhealthyTotal,
			DirectTotal:     upstreamStatus.Summary.DirectTotal,
			SOCKS5Total:     upstreamStatus.Summary.SOCKS5Total,
			Rows:            convertUpstreams(upstreamStatus.Upstreams),
		},
		RecentEvents:      convertRecentEvents(recentEvents.Data.Events),
		Diagnostics:       diagnostics,
		SecurityInventory: securityInventory,
		MeWritersSummary:  meWritersSummary,
	}, partial, nil
}

func (c *Client) fetchUpstreamStatus(ctx context.Context, partial *bool) upstreamStatusResponse {
	var resp upstreamStatusResponse
	if err := c.getJSON(ctx, pathStatsUpstreams, &resp); err != nil {
		*partial = true
		c.logger.Warn("telemt runtime sub-fetch failed", "path", pathStatsUpstreams, "err", err, "ctx_err", ctx.Err())
		return resp
	}
	c.logger.Debug(logTelemtAPICall, "path", pathStatsUpstreams, "configured", resp.Summary.ConfiguredTotal)
	return resp
}

func (c *Client) fetchRecentEvents(ctx context.Context, partial *bool) recentEventsResponse {
	var resp recentEventsResponse
	// Recent events are advisory diagnostics. A temporary read failure must not suppress
	// the core operator snapshot built from health, gates, connections, summary, and DC state.
	if err := c.getJSON(ctx, "/v1/runtime/events/recent", &resp); err != nil {
		*partial = true
		return recentEventsResponse{}
	}
	return resp
}

// markDiagnosticUnavailable flips State/StateReason to the given reason
// only on the first failure (preserves "fresh" -> first failure ordering).
func markDiagnosticUnavailable(diagnostics *RuntimeDiagnostics, reason string) {
	if diagnostics.State == "fresh" {
		diagnostics.State = "unavailable"
		diagnostics.StateReason = reason
	}
}

func (c *Client) buildSlowDiagnostics(ctx context.Context, systemInfo map[string]any, partial *bool) RuntimeDiagnostics {
	diagnostics := RuntimeDiagnostics{
		State:          "fresh",
		StateReason:    "",
		SystemInfoJSON: marshalRawJSON(systemInfo),
	}

	if payload, err := c.getJSONPayload(ctx, "/v1/limits/effective"); err == nil {
		diagnostics.EffectiveLimitsJSON = marshalRawJSON(payload)
	} else {
		*partial = true
		diagnostics.State = "unavailable"
		diagnostics.StateReason = "limits_unavailable"
	}

	if payload, err := c.getJSONPayload(ctx, pathSecurityPosture); err == nil {
		diagnostics.SecurityPostureJSON = marshalRawJSON(payload)
	} else {
		*partial = true
		markDiagnosticUnavailable(&diagnostics, "posture_unavailable")
	}

	minimalAll := map[string]any{}
	if err := c.getJSON(ctx, "/v1/stats/minimal/all", &minimalAll); err == nil {
		diagnostics.MinimalAllJSON = marshalJSON(minimalAll)
	} else {
		*partial = true
		markDiagnosticUnavailable(&diagnostics, "minimal_runtime_unavailable")
	}

	mePool := map[string]any{}
	if err := c.getJSON(ctx, "/v1/runtime/me_pool_state", &mePool); err == nil {
		c.logger.Debug(logTelemtAPICall, "path", "/v1/runtime/me_pool_state")
		diagnostics.MEPoolJSON = marshalJSON(mePool)
	} else {
		*partial = true
		markDiagnosticUnavailable(&diagnostics, "me_pool_unavailable")
	}

	dcsDetail := map[string]any{}
	if err := c.getJSON(ctx, pathStatsDcs, &dcsDetail); err == nil {
		diagnostics.DcsJSON = marshalJSON(dcsDetail)
	} else {
		*partial = true
	}

	return diagnostics
}

func (c *Client) fetchMeWritersSummary(ctx context.Context, partial *bool) RuntimeMeWritersSummary {
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
		*partial = true
		return RuntimeMeWritersSummary{}
	}
	c.logger.Debug(logTelemtAPICall, "path", "/v1/stats/me-writers", "alive_writers", meWritersResp.Summary.AliveWriters)
	return RuntimeMeWritersSummary{
		ConfiguredEndpoints: meWritersResp.Summary.ConfiguredEndpoints,
		AvailableEndpoints:  meWritersResp.Summary.AvailableEndpoints,
		CoveragePct:         meWritersResp.Summary.CoveragePct,
		FreshAliveWriters:   meWritersResp.Summary.FreshAliveWriters,
		FreshCoveragePct:    meWritersResp.Summary.FreshCoveragePct,
		RequiredWriters:     meWritersResp.Summary.RequiredWriters,
		AliveWriters:        meWritersResp.Summary.AliveWriters,
	}
}

func (c *Client) fetchSecurityInventory(ctx context.Context, partial *bool) RuntimeSecurityInventory {
	securityInventory := RuntimeSecurityInventory{
		State:       "fresh",
		StateReason: "",
	}
	whitelist := struct {
		Enabled      bool     `json:"enabled"`
		EntriesTotal int      `json:"entries_total"`
		Entries      []string `json:"entries"`
	}{}
	if err := c.getJSON(ctx, "/v1/security/whitelist", &whitelist); err == nil {
		securityInventory.Enabled = whitelist.Enabled
		securityInventory.EntriesTotal = whitelist.EntriesTotal
		securityInventory.EntriesJSON = marshalJSON(whitelist.Entries)
		return securityInventory
	}
	*partial = true
	securityInventory.State = "unavailable"
	securityInventory.StateReason = "whitelist_unavailable"
	return securityInventory
}

func convertRecentEvents(rows []recentEventEntry) []RuntimeEvent {
	events := make([]RuntimeEvent, 0, len(rows))
	for _, event := range rows {
		events = append(events, RuntimeEvent{
			Sequence:      event.Sequence,
			TimestampUnix: event.TimestampUnix,
			EventType:     event.EventType,
			Context:       event.Context,
		})
	}
	return events
}

func convertUpstreams(rows []struct {
	UpstreamID         int     `json:"upstream_id"`
	RouteKind          string  `json:"route_kind"`
	Address            string  `json:"address"`
	Healthy            bool    `json:"healthy"`
	Fails              int     `json:"fails"`
	EffectiveLatencyMs float64 `json:"effective_latency_ms"`
	Weight             int     `json:"weight"`
	LastCheckAgeSecs   int     `json:"last_check_age_secs"`
	Scopes             any     `json:"scopes"`
}) []RuntimeUpstream {
	upstreams := make([]RuntimeUpstream, 0, len(rows))
	for _, upstream := range rows {
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
	return upstreams
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
	c.logger.Debug(logTelemtAPICall, "path", "/metrics", "user_count", len(parsed.Users))
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

// collectConnectionLinks flattens every non-empty link Telemt returned
// into a single ordered slice. Telemt's tls_domains config emits one
// TLS link per domain (×host); we keep each entry distinct so the
// panel can render them all. Order: TLS → Secure → Classic so the
// strongest mode is first.
func collectConnectionLinks(tlsLinks, secureLinks, classicLinks []string) []string {
	out := make([]string, 0, len(tlsLinks)+len(secureLinks)+len(classicLinks))
	for _, group := range [][]string{tlsLinks, secureLinks, classicLinks} {
		for _, link := range group {
			trimmed := strings.TrimSpace(link)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
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

// formatHTTPErr renders a "<prefix>: <detail>" error string. Centralised so the
// "%s: %s" format literal does not appear at every call site in decodeAPIError
// (Sonar S1192).
func formatHTTPErr(prefix, detail string) error {
	return fmt.Errorf("%s: %s", prefix, detail)
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
			return formatHTTPErr(code, message)
		case code != "":
			return errors.New(code)
		case message != "":
			return formatHTTPErr(fallback, message)
		}
	}

	trimmed := strings.Join(strings.Fields(string(payload)), " ")
	if trimmed != "" {
		return formatHTTPErr(fallback, trimmed)
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
