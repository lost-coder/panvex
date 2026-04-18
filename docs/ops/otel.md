# OpenTelemetry tracing (P3-OBS-01)

The Panvex control-plane emits OpenTelemetry traces for every inbound
HTTP request and every gRPC call from the agent gateway. Tracing is
**off by default** and only activates when `OTEL_EXPORTER_OTLP_ENDPOINT`
is set.

This is the scoped delivery described in P3-OBS-01 of remediation plan
v4. Full browser-to-DB coverage is tracked as a follow-up (see
"Deferred" below).

## Enabling

Export the OTLP/gRPC endpoint of a collector before launching the
control-plane:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=127.0.0.1:4317
./panvex-control-plane
```

### Environment variables

| Variable | Default | Meaning |
| -------- | ------- | ------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | *(unset)* | OTLP/gRPC collector `host:port`. When empty, tracing is a zero-cost no-op. Leading `http://` / `https://` / `grpc://` prefixes are stripped automatically. |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | Set to `false` to require TLS to the collector. |

Service identity is hard-coded to `service.name=panvex-control-plane`;
`service.version` is populated from the build `-ldflags` version string.

## Local collector setup

### Jaeger all-in-one

```bash
docker run --rm -p 16686:16686 -p 4317:4317 \
  -e COLLECTOR_OTLP_ENABLED=true \
  jaegertracing/all-in-one:latest
```

Open <http://localhost:16686> and pick service
`panvex-control-plane` from the dropdown.

### Grafana Tempo

```yaml
# tempo.yaml
server:
  http_listen_port: 3200
distributor:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317
storage:
  trace:
    backend: local
    local:
      path: /tmp/tempo
```

```bash
docker run --rm -p 3200:3200 -p 4317:4317 \
  -v $PWD/tempo.yaml:/etc/tempo.yaml \
  grafana/tempo:latest -config.file=/etc/tempo.yaml
```

Then query via Grafana's Tempo datasource pointed at
`http://localhost:3200`.

## Expected trace structure

Trace roots come from `otelhttp` (HTTP) or `otelgrpc` (gRPC). A typical
HTTP request into the control-plane produces:

```
panvex-api                           (root, otelhttp: "panvex-api")
  HTTP GET /api/agents               (chi route auto-detected)
    <downstream DB/app spans if instrumented>
```

Agent gateway RPCs produce:

```
panvex.gateway.AgentGateway/Stream   (otelgrpc server span)
  ...
```

A custom span `agents.enroll` wraps the enrollment hot path (token
consumption, UUID generation, certificate issuance, first DB write)
with attributes:

- `panvex.node_name`
- `panvex.agent_version`
- `panvex.fleet_group_id`
- `panvex.agent_id` (set once the UUID is generated)

Failures are recorded on the span via `RecordError`.

## Propagation

The control-plane installs the W3C Trace Context propagator and the
Baggage propagator. Agents and upstream proxies that forward
`traceparent` headers on HTTP or gRPC metadata automatically stitch
into the same trace; there is no Panvex-specific header.

## Sampling

Default parent-based sampling is used. An upstream service that decides
to sample (or drop) a trace is respected; in the absence of an inbound
decision everything is sampled. Tune collector-side sampling rather
than the SDK for production.

## Graceful shutdown

The control-plane registers an OTel shutdown hook with a 5-second
timeout ahead of the existing HTTP/gRPC/store shutdowns (LIFO defer
order). Pending span batches are flushed before the process exits.
Unreachable collectors do not extend shutdown past the timeout.

## Deferred (not in this scope)

The following live in follow-up tasks:

- **Frontend fetch instrumentation.** The React dashboard does not yet
  emit browser-side spans, so HTTP traces start at the chi handler.
- **Full DB span coverage.** Only the `agents.enroll` path carries an
  explicit span; sqlc call sites are not wrapped.
- **OTLP metrics.** Panvex keeps Prometheus `/metrics` as the primary
  metrics surface; OTLP metrics are not configured.
- **Custom samplers.** Only the default ParentBased(AlwaysSample) is
  active.

## Troubleshooting

- *"otel init failed; continuing without tracing"* in logs — the
  exporter failed to construct (usually a malformed endpoint). Startup
  is never blocked by tracing init.
- No spans in Jaeger/Tempo — confirm the endpoint is `host:port` (not a
  URL with a path) and that `OTEL_EXPORTER_OTLP_INSECURE` is not set to
  `false` when the collector is plain gRPC.
- Spans truncated at shutdown — increase the shutdown budget in
  `cmd/control-plane/main.go` (currently 5s).
