# Settings System — Architecture

## Overview

Every panel setting is declared once in `internal/controlplane/settings/registry.go`. A reflection walker at startup turns those declarations into typed metadata that drives:

- the **bootstrap loader** (env > config.toml > default) at startup
- the **operational store** (DB cache + typed getters + batch writer) for runtime values
- the **HTTP API** (`/api/settings/schema`, `/api/settings/values`, `/api/settings/restart-status`)
- the **dashboard UI** (`web/src/features/settings/registry/`)
- **codegen artefacts** (`docs/settings/reference.md`, `docs/settings/example.config.toml`)

## Two classes of settings

| Class | Source | UI | Restart |
|---|---|---|---|
| Bootstrap | env > config.toml | read-only with source label | always |
| Operational | DB | editable | per-field flag |

`auth.encryption_key`, ports, TLS, DSN, etc. are bootstrap. Anything an admin reasonably tunes at runtime is operational.

## Adding a new bootstrap setting

1. Append a field to `Bootstrap` in `internal/controlplane/settings/registry.go` with a `setting:"…"` tag including `env=` (and usually `toml=`).
2. Run `make gen-settings` to regenerate the reference + example config.
3. Read the value at startup from `boot.YourField`.
4. Test that the loader picks up env, toml, and default.

## Adding a new operational setting

1. Append a field to `Operational` in the registry with `store=runtime_settings` (or `store=panel_settings.<column>` if there's a typed column).
2. Run `make gen-settings`.
3. Add a typed getter on `OperationalStore` in `store.go` (use `durationByName` / `intByName` helpers).
4. Replace the call site to read from the store getter instead of a constant.
5. The dashboard auto-renders the new field by namespace (no UI work required).

## Codegen

`make gen-settings` regenerates:
- `internal/controlplane/settings/gen/schema.json` — served by `GET /api/settings/schema`
- `docs/settings/reference.md` — operator-facing reference
- `docs/settings/example.config.toml` — minimal example bootstrap config

A pre-push hook fails if these drift. Run `make gen-settings` and commit if you change registry tags.

## Tests

- `internal/controlplane/settings/*_test.go` covers tag parsing, validation, loader precedence, store round-trips, and codegen idempotence.
- `internal/controlplane/server/http_settings_*_test.go` covers the HTTP handlers and end-to-end flows.

## See also

- Design spec: `docs/superpowers/specs/2026-05-07-settings-foundation-design.md`
- Audit spec: `docs/superpowers/specs/2026-05-07-settings-audit-design.md`
- UI spec: `docs/superpowers/specs/2026-05-07-settings-ui-design.md`
