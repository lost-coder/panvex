package telemt

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
