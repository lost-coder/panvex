package telemt

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"
)

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

// fetchSlowRuntimeState reads the heavier Telemt endpoints that do not need live refresh on every snapshot.
//
// Returns the collected slow state, a partial flag (true when at least one
// advisory sub-endpoint failed but the core system/info payload still arrived),
// and a non-nil error only when the required /v1/system/info payload itself is
// unreachable. See P2-REL-07.
//
// P-10: the dependent /v1/system/info call is fetched first because
// buildSlowDiagnostics needs its payload. Once it returns, the four remaining
// independent sub-fetches (upstreams, recent events, me-writers, security
// inventory) plus the diagnostics builder run in parallel via errgroup. The
// outer caching contract (loadSlowRuntimeStateForFetch) is unchanged: the
// TTL still wraps a single fetchSlowRuntimeState invocation.
func (c *Client) fetchSlowRuntimeState(ctx context.Context) (slowRuntimeState, bool, error) {
	systemInfo, err := c.getJSONPayload(ctx, "/v1/system/info")
	if err != nil {
		return slowRuntimeState{}, true, err
	}
	// Each parallel sub-fetch writes its own *bool partial flag; we OR
	// them together after Wait. This avoids racing on a shared *bool from
	// the existing helper signatures and keeps those helpers untouched.
	var (
		partialUpstream    bool
		partialRecent      bool
		partialDiagnostics bool
		partialMeWriters   bool
		partialSecurity    bool

		upstreamStatus    upstreamStatusResponse
		recentEvents      recentEventsResponse
		diagnostics       RuntimeDiagnostics
		meWritersSummary  RuntimeMeWritersSummary
		securityInventory RuntimeSecurityInventory
	)

	eg, gctx := errgroup.WithContext(ctx)
	eg.SetLimit(runtimeFetchConcurrency)
	eg.Go(func() error {
		upstreamStatus = c.fetchUpstreamStatus(gctx, &partialUpstream)
		return nil
	})
	eg.Go(func() error {
		recentEvents = c.fetchRecentEvents(gctx, &partialRecent)
		return nil
	})
	eg.Go(func() error {
		diagnostics = c.buildSlowDiagnostics(gctx, systemInfo, &partialDiagnostics)
		return nil
	})
	eg.Go(func() error {
		meWritersSummary = c.fetchMeWritersSummary(gctx, &partialMeWriters)
		return nil
	})
	eg.Go(func() error {
		securityInventory = c.fetchSecurityInventory(gctx, &partialSecurity)
		return nil
	})
	_ = eg.Wait()

	partial := partialUpstream || partialRecent || partialDiagnostics || partialMeWriters || partialSecurity

	return slowRuntimeState{
		Version:       jsonString(systemInfo["version"]),
		UptimeSeconds: jsonFloat(systemInfo["uptime_seconds"]),
		Upstreams: RuntimeUpstreamSummary{
			ConfiguredTotal:  upstreamStatus.Summary.ConfiguredTotal,
			HealthyTotal:     upstreamStatus.Summary.HealthyTotal,
			UnhealthyTotal:   upstreamStatus.Summary.UnhealthyTotal,
			DirectTotal:      upstreamStatus.Summary.DirectTotal,
			SOCKS4Total:      upstreamStatus.Summary.SOCKS4Total,
			SOCKS5Total:      upstreamStatus.Summary.SOCKS5Total,
			ShadowsocksTotal: upstreamStatus.Summary.ShadowsocksTotal,
			Rows:             convertUpstreams(upstreamStatus.Upstreams),
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
