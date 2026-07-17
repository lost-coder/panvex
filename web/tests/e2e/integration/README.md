# Backend-integration E2E

These tests exercise the full stack — real control-plane + real SQLite
— instead of mocking `/api/*` like the Chromium smoke suite. They
catch contract drift that the frontend mocks could never see (auth
middleware, session store, audit persistence).

## Running locally

1. Build the embedded UI bundle, then the control-plane binary with the
   `embeddedui` tag. Without the tag `embeddedUIFiles()` returns `nil`
   (`cmd/control-plane/ui_embed_stub.go`) and every page — including
   `/login` — 404s, so the suite has nothing to load:
   ```bash
   npm run build:embed
   cd ..
   go build -tags embeddedui -o .tmp/control-plane ./cmd/control-plane
   ```

2. Bootstrap a first admin into a **file-backed** SQLite DB. The
   bootstrap and the server in step 3 must point at the exact same DSN —
   `file::memory:?cache=shared` cannot share state across two separate
   processes (each process gets its own connection pool; SQLite's
   shared-cache mode shares a cache within one process, not across
   process boundaries). `-password` also requires an explicit insecure-flag
   opt-in outside an interactive TTY:
   ```bash
   PANVEX_BOOTSTRAP_ALLOW_INSECURE_FLAG=1 ./.tmp/control-plane bootstrap-admin \
     -username admin -password e2e-secret \
     -storage-driver sqlite \
     -storage-dsn .tmp/e2e.db
   ```

3. Start the control-plane against that same file, via env vars — `serve`
   (the implicit default subcommand) does not accept `-storage-driver` /
   `-storage-dsn`; it only reads `PANVEX_STORAGE_*`. `PANVEX_ENCRYPTION_KEY`
   is required at boot (settings load fails without it):
   ```bash
   PANVEX_ENCRYPTION_KEY=e2e-throwaway-key \
   PANVEX_STORAGE_DRIVER=sqlite \
   PANVEX_STORAGE_DSN=.tmp/e2e.db \
   PANVEX_HTTP_ADDR=:18080 \
   ./.tmp/control-plane &
   ```

4. From `web/`, run the integration suite against the running backend:
   ```bash
   npm run test:e2e:integration
   ```

This recipe (embed build → tagged binary → same-DSN bootstrap+serve → suite)
was run end-to-end while fixing this doc; all four steps and
`login.int.spec.ts` passed.

## CI shape

CI runs this suite natively (no docker-compose): the
`frontend-e2e-integration` job in `.github/workflows/ci.yml` builds the
control-plane with the embedded UI (`-tags embeddedui`), bootstraps
`admin/e2e-secret` into a throwaway SQLite DB under `.tmp/e2e/`, starts
the binary on `:18080`, waits for `/readyz`, and runs
`npx playwright test -c playwright.integration.config.ts`. Traces and
the panel log are uploaded as artifacts on failure.

## Why it's separate from the mock smoke

The smoke suite runs on every PR in < 2 minutes with no backend.
Integration E2E takes 3–5× longer and needs a healthy binary build. It
is **not** gated on main-merge or a nightly cron — `.github/workflows/ci.yml`
has no `schedule:` trigger at all, and `frontend-e2e-integration` runs
on the same `push`/`pull_request` triggers as everything else in the
file, i.e. on every PR into `main` (and every push to `main`), just like
the mock smoke job. It's a separate job (not a separate cadence) so a
slow integration failure doesn't block the fast smoke feedback. Bugs
caught here are almost always auth/session issues that mocks can't
reproduce.

## What's intentionally not here

* **Agent-level tests** — an agent needs gRPC + TLS + a Telemt target,
  which is a separate integration tier (`cmd/agent` tests in Go).
* **PostgreSQL variant** — SQLite covers 95% of the surface; add a
  Postgres job only if a handler diverges between drivers.
* **Visual snapshots on integration** — real-data shots are too noisy
  to diff. Visual regression stays on the mocked suite.
