package telemt

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

// fetchRuntimeStateRaw bundles the per-endpoint payloads collected during
// FetchRuntimeState so the assembly step can remain a straight-line
// projection without re-declaring the inner struct types.
type fetchRuntimeStateRaw struct {
	posture struct {
		// IN-L6: telemt's /v1/security/posture emits only api_read_only; the
		// old read_only field was always its zero value (dead) — removed.
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
		ConnectionsTotal         uint64                `json:"connections_total"`
		ConnectionsBadTotal      uint64                `json:"connections_bad_total"`
		HandshakeTimeoutsTotal   uint64                `json:"handshake_timeouts_total"`
		ConfiguredUsers          int                   `json:"configured_users"`
		ConnectionsBadByClass    []ConnectionClassStat `json:"connections_bad_by_class"`
		HandshakeFailuresByClass []ConnectionClassStat `json:"handshake_failures_by_class"`
	}
	dcs        []RuntimeDC
	slowData   slowRuntimeState
	users      []ClientUsage
	systemLoad RuntimeSystemLoad
}

// upstreamStatusResponse mirrors the /v1/stats/upstreams payload that
// fetchSlowRuntimeState consumes. Hoisted to package scope so helper
// functions can return it without redeclaring the anonymous struct.
type upstreamStatusResponse struct {
	Summary struct {
		ConfiguredTotal  int `json:"configured_total"`
		HealthyTotal     int `json:"healthy_total"`
		UnhealthyTotal   int `json:"unhealthy_total"`
		DirectTotal      int `json:"direct_total"`
		SOCKS4Total      int `json:"socks4_total"`
		SOCKS5Total      int `json:"socks5_total"`
		ShadowsocksTotal int `json:"shadowsocks_total"`
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

// userQuotaResponse mirrors the GET /v1/users/quota payload (Telemt
// 3.4.12+). Telemt filters the response to users with
// data_quota_bytes > 0; absent users mean "no quota configured".
// On older Telemt (< 3.4.12) the endpoint returns 404; callers must
// treat that as "no quota data available" and not as a hard error.
type userQuotaResponse struct {
	Users []struct {
		Username           string `json:"username"`
		DataQuotaBytes     uint64 `json:"data_quota_bytes"`
		UsedBytes          uint64 `json:"used_bytes"`
		LastResetEpochSecs uint64 `json:"last_reset_epoch_secs"`
	} `json:"users"`
}
