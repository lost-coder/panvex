# Panvex metrics — Prometheus / Grafana

The control-plane exposes a Prometheus `/metrics` endpoint for runtime
observability. This document describes how to enable the endpoint, scrape it,
and what metrics are currently emitted.

Full dashboards are not yet provided — downstream remediation tasks
(P2-OBS-03, P2-REL-06, P2-LOG-10, P2-PERF-05, P3-OBS-01) will flesh out
specific counters and accompanying Grafana panels.

## Enabling the endpoint

`/metrics` is **opt-in**. It is only registered when the control-plane is
started with a non-empty scrape token. The token is read from the environment
only (never a CLI flag, so it cannot leak into `ps` / shell history):

```bash
export PANVEX_METRICS_SCRAPE_TOKEN="$(openssl rand -hex 32)"
./panvex-control-plane
```

When the variable is empty or unset, a request to `/metrics` returns **404**
(the route is not registered at all). When it is set, callers must present
`Authorization: Bearer <token>` with a byte-for-byte match; anything else
returns **401**.

## Scraping from the command line

```bash
curl -sS -H "Authorization: Bearer $PANVEX_METRICS_SCRAPE_TOKEN" \
    https://panvex.example.com/metrics
```

## Scraping from Prometheus

```yaml
scrape_configs:
  - job_name: panvex-control-plane
    metrics_path: /metrics
    scheme: https
    authorization:
      type: Bearer
      credentials: ${PANVEX_METRICS_SCRAPE_TOKEN}
    static_configs:
      - targets: ['panvex.example.com:443']
```

## Exposed metrics

All metrics use the `panvex_` prefix.

| Metric | Type | Labels | Notes |
| --- | --- | --- | --- |
| `panvex_http_request_duration_seconds` | Histogram | `method`, `path`, `status` | Request latency, default buckets. `path` is the chi route template (e.g. `/api/clients/{id}`), never a raw URL. `status` is bucketed (`2xx`/`3xx`/`4xx`/`5xx`). |
| `panvex_http_requests_total` | Counter | `method`, `path`, `status` | Per-route request count (same labels as the histogram). |
| `panvex_agent_connected` | Gauge | — | Number of agents currently tracked by the presence service. |
| `panvex_batch_queue_depth` | Gauge | `buffer` | Depth of each batch-writer buffer (`agents`, `instances`, `metrics`, `server_load`, `dc_health`, `client_ips`, `telemetry`). |
| `panvex_batch_flush_errors_total` | Counter | `buffer`, `error_type` | Flush failures per buffer, bucketed into `transient` vs `persistent` (downstream task P2-REL-06 wires this). |
| `panvex_event_hub_drop_total` | Counter | — | Events dropped because a subscriber channel was full. |
| `panvex_event_hub_subscribers` | Gauge | — | Current number of event-hub subscribers. |
| `panvex_job_queue_depth` | Gauge | — | Jobs in `queued` or `running` state. |
| `panvex_lockout_active` | Gauge | — | Usernames currently serving a login lockout. |
| `panvex_audit_buffer_depth` | Gauge | — | Depth of the in-memory audit-event buffer. Downstream task P2-LOG-10 will replace the in-memory ring with a persistent buffer but keeps this metric name stable. |
| `panvex_unsigned_update_fallback_total` | Counter | — | Panel-update applications that had to fall back to an unsigned manifest. |

### Cardinality notes

Label values are deliberately bounded. In particular the HTTP metrics
**never** include user IDs, agent IDs, session IDs, client IDs, or raw request
paths — `path` is always the chi-registered template so `/api/clients/abc`
and `/api/clients/def` both fold into a single `/api/clients/{id}` series.
Unmatched paths (404, OPTIONS preflight, static UI) carry the sentinel label
`path="unmatched"`.

## Grafana dashboards

TBD — downstream observability tasks will ship JSON dashboards alongside
their metric additions.
