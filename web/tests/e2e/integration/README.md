# Backend-integration E2E

These tests exercise the full stack — real control-plane + real SQLite
— instead of mocking `/api/*` like the Chromium smoke suite. They
catch contract drift that the frontend mocks could never see (auth
middleware, session store, audit persistence).

## Running locally

1. Build the control-plane:
   ```bash
   cd ..
   go build -o .tmp/control-plane ./cmd/control-plane
   ```

2. Bootstrap a first admin:
   ```bash
   ./.tmp/control-plane bootstrap-admin -username admin -password e2e-secret
   ```

3. Start the control-plane with a temporary SQLite DB:
   ```bash
   PANVEX_E2E=1 ./.tmp/control-plane \
     -storage-driver sqlite \
     -storage-dsn file::memory:?cache=shared \
     -http-listen :18080 &
   ```

4. From `web/`, run the integration suite against the running backend:
   ```bash
   npm run test:e2e:integration
   ```

## CI shape

The CI variant wraps the steps above in a docker-compose file —
`tests/e2e/integration/docker-compose.yml` spins up:

* `control-plane` (amd64 Go binary, SQLite in-memory)
* `e2e-runner` (node image, runs `npm run test:e2e:integration`)

The compose file is scaffolded but not committed to avoid tying this
PR to a specific CI runner image. Populate it before enabling the
`web-e2e-integration` workflow.

## Why it's separate from the mock smoke

The smoke suite runs on every PR in < 2 minutes with no backend.
Integration E2E takes 3–5× longer and needs a healthy binary build,
so we gate it on main-merge + nightly cron instead of every PR. Bugs
caught here are almost always auth/session issues that mocks can't
reproduce.

## What's intentionally not here

* **Agent-level tests** — an agent needs gRPC + TLS + a Telemt target,
  which is a separate integration tier (`cmd/agent` tests in Go).
* **PostgreSQL variant** — SQLite covers 95% of the surface; add a
  Postgres job only if a handler diverges between drivers.
* **Visual snapshots on integration** — real-data shots are too noisy
  to diff. Visual regression stays on the mocked suite.
