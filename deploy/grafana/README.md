# Panvex Grafana dashboards

Two starter dashboards built against the Prometheus metrics exported by the
control-plane at `/metrics` (see `internal/controlplane/server/metrics.go`).
They pair with the alert rules in `deploy/prometheus/alerts.yml`.

| File | UID | Covers |
|---|---|---|
| `dashboards/panvex-fleet.json` | `panvex-fleet` | Agents connected, inbound message drops, event-hub subscribers/drops, reverse-mode transport (supervisors / bootstrap / cert-pin). |
| `dashboards/panvex-control-plane.json` | `panvex-control-plane` | HTTP duration/throughput, batch-writer queue/flush/persist/retries, DB pool open/in-use/idle/wait, rate-limit rejections, recovered goroutine panics, job/audit/lockout depths. |

Every panel references a dashboard variable `${DS_PROMETHEUS}` of type
`datasource`, so on import Grafana asks which Prometheus instance to bind.

## Prerequisites

The control-plane only serves `/metrics` when `PANVEX_METRICS_SCRAPE_TOKEN`
is set. Configure Prometheus to scrape it with that bearer token, e.g.:

```yaml
scrape_configs:
  - job_name: panvex
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials: ${PANVEX_METRICS_SCRAPE_TOKEN}
    static_configs:
      - targets: ["panvex:8080"]
```

## Import via the UI

1. Grafana → Dashboards → New → Import.
2. Upload one of the JSON files (or paste its contents).
3. When prompted, select your Prometheus data source for `DS_PROMETHEUS`.
4. Import. Repeat for the second dashboard.

## Import via provisioning (recommended for prod)

Mount this directory and the provisioning config into the Grafana container:

```yaml
# docker-compose snippet
grafana:
  image: grafana/grafana:latest
  volumes:
    - ./deploy/grafana/dashboards:/var/lib/grafana/dashboards:ro
    - ./deploy/grafana/provisioning:/etc/grafana/provisioning:ro
```

`provisioning/dashboards/panvex.yaml` tells Grafana to load every JSON file in
`/var/lib/grafana/dashboards`. `provisioning/datasources/prometheus.yaml` is an
optional sample datasource named `Prometheus` — adjust the `url` to your
Prometheus and Grafana will resolve `${DS_PROMETHEUS}` to it automatically.

## Metric names

All PromQL in these dashboards uses the actual exported names, all prefixed
`panvex_`:

- `panvex_agent_connected`, `panvex_agent_inbound_drops_total`
- `panvex_event_hub_subscribers`, `panvex_event_hub_drop_total`
- `panvex_http_requests_total`, `panvex_http_request_duration_seconds` (`_bucket`)
- `panvex_batch_queue_depth`, `panvex_batch_flush_duration_seconds` (`_bucket`),
  `panvex_batch_persist_errors_total`, `panvex_batch_persist_retries_total`
- `panvex_db_pool_open_connections`, `panvex_db_pool_in_use_connections`,
  `panvex_db_pool_idle_connections`, `panvex_db_pool_max_open_connections`,
  `panvex_db_pool_wait_total`, `panvex_db_pool_wait_seconds_total`
- `panvex_ratelimit_rejected_total`, `panvex_goroutine_panic_recovered_total`
- `panvex_job_queue_depth`, `panvex_lockout_active` (audit-pipeline backlog is
  surfaced by `panvex_batch_queue_depth{buffer="audit"}`)
- `panvex_outbound_supervisors_total`, `panvex_bootstrap_attempts_total`,
  `panvex_agent_cert_pin_total`
