package telemt

import "context"

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
	upstreams := raw.slowData.Upstreams
	if c.upstreamRate != nil {
		pct, known := c.upstreamRate.Rate()
		// Use SetFailRatePct5m so the (rate, known) pair always moves
		// together — see RuntimeUpstreamSummary doc comment.
		if known {
			upstreams.SetFailRatePct5m(&pct)
		} else {
			upstreams.SetFailRatePct5m(nil)
		}
	}
	c.upstreamCountersMu.RLock()
	if c.hasUpstreamCounters {
		upstreams.ConnectAttemptTotal = c.latestUpstreamCounters.Attempt
		upstreams.ConnectSuccessTotal = c.latestUpstreamCounters.Success
		upstreams.ConnectFailTotal = c.latestUpstreamCounters.Fail
		upstreams.ConnectFailfastTotal = c.latestUpstreamCounters.Failfast
	}
	c.upstreamCountersMu.RUnlock()

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
		Upstreams:         upstreams,
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

// markDiagnosticUnavailable flips State/StateReason to the given reason
// only on the first failure (preserves "fresh" -> first failure ordering).
func markDiagnosticUnavailable(diagnostics *RuntimeDiagnostics, reason string) {
	if diagnostics.State == "fresh" {
		diagnostics.State = "unavailable"
		diagnostics.StateReason = reason
	}
}
