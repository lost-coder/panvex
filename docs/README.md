# Panvex documentation

Technical reference for the Panvex control-plane, agents, and operations.

## API reference

- [`api/openapi.yaml`](api/openapi.yaml) — OpenAPI 3.0 spec for the HTTP
  control-plane API (top ~20 critical operations). Render with Swagger UI,
  Redoc, or any OpenAPI 3 client.
- [`api/gateway.proto.md`](api/gateway.proto.md) — narrative reference for
  the `AgentGateway` gRPC service, message shapes, and wire-level tuning
  (mTLS, keepalive, 16 MiB message cap).

## Architecture

- [`architecture/adr/`](architecture/adr/) — architecture decision records.
  See [`adr/README.md`](architecture/adr/README.md) for the index.

## Operations

- [`ops/runbook.md`](ops/runbook.md) — operational runbook for the
  control-plane and agents.
- [`ops/migrations.md`](ops/migrations.md) — database migration framework.
- [`ops/release-signing.md`](ops/release-signing.md) — release binary
  signing with cosign.
- [`ops/grafana/`](ops/grafana/) — Grafana dashboards for the Prometheus
  metrics exposed at `/metrics`.
