package telemt

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

// ErrTelemtCoreUnreachable reports that EVERY core sub-fetch (runtime
// gates, stats summary, connections summary) failed within one
// FetchRuntimeState cycle — Telemt did not answer anything the agent
// could build a meaningful snapshot from. cmd/agent's runtime poll loop
// matches it with errors.Is to drive the unreachable-snapshot path
// (threshold, backoff, BuildRuntimeUnreachableSnapshot). Partial
// degradation (some endpoints up) still returns a nil error with
// Partial=true. Audit 2026-07-02 #4.
var ErrTelemtCoreUnreachable = errors.New("telemt core endpoints unreachable")

// runtimeCorePaths are the sub-fetches whose COLLECTIVE failure means
// "Telemt is down" rather than "snapshot is partial".
var runtimeCorePaths = map[string]bool{
	pathRuntimeGates:              true,
	pathStatsSummary:              true,
	pathRuntimeConnectionsSummary: true,
}

// runtimeFetchConcurrency bounds the number of in-flight Telemt sub-fetches
// during a single FetchRuntimeState cycle. Telemt is a local loopback
// service; 8 concurrent goroutines saturate its event loop without
// inducing tail-latency from queueing on its single accept thread (P-10).
const runtimeFetchConcurrency = 8

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
//
// Exception: when ALL core sub-fetches (see runtimeCorePaths) fail,
// FetchRuntimeState returns ErrTelemtCoreUnreachable instead of a
// zero-valued partial state, so the poll loop can drive its
// unreachable-snapshot path.
func (c *Client) FetchRuntimeState(ctx context.Context) (RuntimeState, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultFetchRuntimeStateDeadline)
		defer cancel()
	}

	// partial is shared across the parallel sub-fetches and is the only
	// piece of mutable state read after collectRuntimeStateRaw returns.
	// atomic.Bool keeps the markPartial/setPartial closures race-free
	// without an extra mutex.
	var partial atomic.Bool
	var coreMu sync.Mutex
	coreFailed := make(map[string]bool, len(runtimeCorePaths))
	markPartial := func(path string, err error) {
		partial.Store(true)
		if runtimeCorePaths[path] {
			coreMu.Lock()
			coreFailed[path] = true
			coreMu.Unlock()
		}
		c.logger.Warn("telemt runtime sub-fetch failed",
			"path", path,
			"err", err,
			"ctx_err", ctx.Err(),
		)
	}
	setPartial := func() { partial.Store(true) }

	raw := c.collectRuntimeStateRaw(ctx, markPartial, setPartial)

	coreMu.Lock()
	coreDown := len(coreFailed) == len(runtimeCorePaths)
	coreMu.Unlock()
	if coreDown {
		return RuntimeState{}, ErrTelemtCoreUnreachable
	}
	return c.assembleRuntimeState(raw, partial.Load()), nil
}

// collectRuntimeStateRaw runs every Telemt sub-fetch FetchRuntimeState
// needs, marking partial-failures via markPartial (logs + flags) or
// setPartial (flag only). It is a thin orchestration wrapper so
// FetchRuntimeState's flow stays linear.
//
// P-10: independent sub-fetches run in parallel via errgroup, bounded at
// runtimeFetchConcurrency. Each helper writes to its own field on `raw`,
// so per-subfetch state is partition-safe; the only shared mutable
// surface is the partial flag (atomic) and the slow-state cache (already
// guarded by c.mu inside loadSlowRuntimeStateForFetch). Errors are NOT
// returned to errgroup — every helper records failure via markPartial
// instead — preserving the original "first failure does not abort other
// fetches" semantic. errgroup is used purely as a bounded WaitGroup.
func (c *Client) collectRuntimeStateRaw(ctx context.Context, markPartial func(string, error), setPartial func()) fetchRuntimeStateRaw {
	raw := fetchRuntimeStateRaw{}

	// dcs lives in a field the parallel goroutines write directly.
	// The slow-state collector writes raw.slowData; it's the single writer
	// for that field too. No cross-goroutine field aliasing.

	eg, gctx := errgroup.WithContext(ctx)
	eg.SetLimit(runtimeFetchConcurrency)

	eg.Go(func() error {
		c.fetchHealth(gctx, markPartial)
		return nil
	})
	eg.Go(func() error {
		c.fetchSecurityPosture(gctx, &raw, markPartial)
		return nil
	})
	eg.Go(func() error {
		c.fetchRuntimeGates(gctx, &raw, markPartial)
		return nil
	})
	eg.Go(func() error {
		c.fetchInitialization(gctx, &raw, markPartial)
		return nil
	})
	eg.Go(func() error {
		c.fetchConnectionSummary(gctx, &raw, markPartial)
		return nil
	})
	eg.Go(func() error {
		c.fetchSummary(gctx, &raw, markPartial)
		return nil
	})

	// fetchDCs returns its slice; capture into raw.dcs from the goroutine.
	eg.Go(func() error {
		dcs := c.fetchDCs(gctx, markPartial)
		raw.dcs = dcs
		return nil
	})

	// Slow runtime state owns the heaviest endpoints and uses an internal
	// TTL cache; running it in parallel with the fast endpoints does not
	// disturb the cache contract because loadSlowRuntimeStateForFetch is
	// already mutex-protected internally.
	eg.Go(func() error {
		c.collectSlowRuntimeState(gctx, &raw, markPartial, setPartial)
		return nil
	})

	if c.systemLoadSampler != nil {
		eg.Go(func() error {
			// Keep whatever probes succeeded even on partial failure; the
			// sampler flags incomplete fields via RuntimeSystemLoad.Partial
			// and returns the joined probe errors (IN-L3).
			load, err := c.systemLoadSampler(gctx)
			raw.systemLoad = load
			if err != nil {
				markPartial("system_load_sampler", err)
			}
			return nil
		})
	}

	// errgroup.Wait returns the first non-nil error from any Go call.
	// Every helper above returns nil unconditionally, so Wait will only
	// surface a context error propagated by gctx — which the
	// partial-snapshot semantics already covers via markPartial. The _ =
	// is intentional; nothing actionable to surface here.
	_ = eg.Wait()
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
	if err := c.getJSON(ctx, pathStatsSummary, &raw.summary); err != nil {
		markPartial(pathStatsSummary, err)
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
		Version:       raw.slowData.Version,
		ReadOnly:      raw.posture.APIReadOnly,
		UptimeSeconds: raw.slowData.UptimeSeconds,
		Connections:   raw.connectionSummary.Data.Totals.CurrentConnections,
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
			ConnectionsTotal:         raw.summary.ConnectionsTotal,
			ConnectionsBadTotal:      raw.summary.ConnectionsBadTotal,
			HandshakeTimeoutsTotal:   raw.summary.HandshakeTimeoutsTotal,
			ConfiguredUsers:          raw.summary.ConfiguredUsers,
			ConnectionsBadByClass:    convertClassStats(raw.summary.ConnectionsBadByClass),
			HandshakeFailuresByClass: convertClassStats(raw.summary.HandshakeFailuresByClass),
		},
		DCs:               raw.dcs,
		Upstreams:         upstreams,
		RecentEvents:      raw.slowData.RecentEvents,
		Diagnostics:       raw.slowData.Diagnostics,
		SecurityInventory: raw.slowData.SecurityInventory,
		MeWritersSummary:  raw.slowData.MeWritersSummary,
		SystemLoad:        raw.systemLoad,
		Partial:           partial,
	}
}

// convertClassStats maps the raw Telemt class-count rows into the public
// ConnectionClassStat slice. Drops rows with empty class to be defensive
// against malformed payloads while keeping Telemt-driven ordering.
func convertClassStats(rows []ConnectionClassStat) []ConnectionClassStat {
	if len(rows) == 0 {
		return nil
	}
	out := make([]ConnectionClassStat, 0, len(rows))
	for _, r := range rows {
		if r.Class == "" {
			continue
		}
		out = append(out, ConnectionClassStat{Class: r.Class, Total: r.Total})
	}
	return out
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
