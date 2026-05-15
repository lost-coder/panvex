package telemt

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
	// Class-based breakdown of bad connections and handshake failures,
	// added by Telemt 3.4.10 (`SummaryData.connections_bad_by_class` and
	// `.handshake_failures_by_class`). The class set is open (e.g.
	// `unknown_tls_sni`, `expected_64_got_0_unexpected_eof`, `other`);
	// callers must not enforce a known-class allow-list. Empty when the
	// agent runs against Telemt < 3.4.10.
	ConnectionsBadByClass    []ConnectionClassStat
	HandshakeFailuresByClass []ConnectionClassStat
}

// ConnectionClassStat is one (class, total) row from Telemt's classified
// bad-connection and handshake-failure counters. See RuntimeSummary.
type ConnectionClassStat struct {
	Class string
	Total uint64
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
	ConfiguredTotal  int
	HealthyTotal     int
	UnhealthyTotal   int
	DirectTotal      int
	SOCKS4Total      int
	SOCKS5Total      int
	ShadowsocksTotal int
	Rows             []RuntimeUpstream

	// Direct-mode signals — populated from UpstreamRateTracker on each fetch.
	//
	// The (FailRatePct5m, FailRateKnown) pair is the "nil-is-unknown" pattern
	// expressed via two parallel fields because the wire format (proto fields
	// 7+8 on RuntimeUpstreamSnapshot, JSON tags fail_rate_pct_5m + fail_rate_known)
	// keeps them split. Inside Go, prefer FailRatePct5mPtr() / SetFailRatePct5m()
	// so reads/writes stay in lockstep — never set one field without the other.
	FailRatePct5m        float64
	FailRateKnown        bool
	ConnectAttemptTotal  uint64
	ConnectSuccessTotal  uint64
	ConnectFailTotal     uint64
	ConnectFailfastTotal uint64
}

// FailRatePct5mPtr returns the 5-minute upstream connect fail-rate as a
// pointer, with nil indicating "unknown" (FailRateKnown == false). Use this
// instead of reading FailRatePct5m and FailRateKnown directly to avoid
// desync bugs where one field is set without the other.
func (s RuntimeUpstreamSummary) FailRatePct5mPtr() *float64 {
	if !s.FailRateKnown {
		return nil
	}
	v := s.FailRatePct5m
	return &v
}

// SetFailRatePct5m updates FailRatePct5m and FailRateKnown together: a nil
// pointer marks the rate unknown (and zeroes FailRatePct5m), a non-nil
// pointer stores the value and flips FailRateKnown to true. Always prefer
// this over touching the parallel fields directly.
func (s *RuntimeUpstreamSummary) SetFailRatePct5m(rate *float64) {
	if rate == nil {
		s.FailRatePct5m = 0
		s.FailRateKnown = false
		return
	}
	s.FailRatePct5m = *rate
	s.FailRateKnown = true
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
