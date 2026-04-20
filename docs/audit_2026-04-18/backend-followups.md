# Backend follow-ups from Phase-7 UI work

Bugs and rough edges that surfaced while wiring the new dashboard
against a real 4-agent fleet. Not fixed in this PR to keep the front-end
review cycle fast; track each as its own backend ticket.

## 1. RuntimeEvents are snapshots, not a delta

**Where:** `internal/agent/runtime/*` → `Agent.Runtime.RecentEvents`
(surfaced on `/api/telemetry/dashboard` as `recent_events` and
`recent_runtime_events`).

**Symptom:** the web dashboard shows duplicated "Accepting new
connections" / "Stopped accepting new connections" pairs — identical
time, identical message, different sequence numbers. Every heartbeat
ships the same tail of recent events, so the control-plane stores the
same rows on every tick.

**Expected:** agent should ship *new* events since last heartbeat (a
delta), or include a server-side dedupe window in
`dashboardRecentEvents`. Preferred: agent sends deltas; control-plane
keeps only the last N non-duplicate entries per event_type+context
within a time window (e.g. 2 minutes).

**Impact:** Recent Events on dashboard shows noise instead of signal.

**Scope:** backend only. Frontend correctly renders whatever the
payload contains.

## 2. ServerListItem.ip is never populated

**Where:** `web/src/shared/api/transforms/servers.ts` →
`summaryToListItem`. The `ip` field exists on `ServerListItem` but is
optional and never set because `TelemetryServerSummary` / `Agent` from
`/api/telemetry/servers` does not carry a node IP.

**Symptom:** the Servers table leaves the "IP under the node name"
line empty on every row. The handoff design shows the IP there.

**Expected:** either surface a public/mgmt IP on the agent record
(probably the `node_name`-resolvable address used for enrollment) or
expose the last-seen TLS peer IP. Telemt itself knows the gRPC client
address — plumbing it through would be cheap.

**Scope:** backend — add the field to `Agent` and populate it in the
telemetry summary builder. The frontend already has the render slot
ready (`ServerListItem.ip`), so picking up the new field is a one-line
change in `summaryToListItem`.

## 3. ClientIPHistory has no GeoIP enrichment

**Where:** `GET /api/clients/{id}/history/ips` → `ClientIPEntry` currently
carries `{ AgentID, IPAddress, FirstSeen, LastSeen }` only. Storage layer
`ClientIPHistoryRecord` (`internal/controlplane/storage/`) + the
handler in `internal/controlplane/server/http_history.go` don't run any
geo lookup on the recorded address.

**Symptom:** the redesigned client detail page reserves columns for
Country / City / ASN but must render them as "—" placeholders until the
backend populates them. Operators triaging abuse patterns have to copy
each IP into a third-party service manually.

**Expected:** join against a MaxMind GeoLite2 (or equivalent) database
at query time or at ingest — add `country_code`, `country_name`,
`city`, and `asn` fields on `ClientIPEntry`. Ingest-time lookup trades
a bit of extra rows per batch for zero query-time latency; either
trade-off is acceptable.

**Scope:** backend. Frontend already has the render slots; adding the
four optional fields to the API response will light them up
automatically.

## 4. Client detail GET does not return the current secret

**Where:** `internal/controlplane/server/http_clients.go` →
`clientDetailResponse.Secret` is declared `json:"secret,omitempty"`.
It is populated only on `POST /clients` (create) and
`POST /clients/{id}/rotate-secret` responses — the regular
`GET /clients/{id}` leaves it blank.

**Symptom:** the new client detail page has a "Secret" card with a
Reveal toggle, but after the first navigation away the secret is no
longer in the API payload, so revealing shows an empty value.
Operators who need to re-distribute a link have to rotate (which
invalidates the old one) just to see it.

**Expected:** expose a dedicated `GET /clients/{id}/secret` (requires
admin role, audit-logged) OR include the secret on `GET /clients/{id}`
for admins. The frontend already has the render slot and a Copy button
next to it.

**Scope:** backend only. Frontend works as soon as the field is
returned in the detail response.

## 5. clientDeploymentResponse has no node_name

**Where:** `internal/controlplane/server/http_clients.go` →
`clientDeploymentResponse` only carries `agent_id`, not `node_name`.

**Symptom:** the Deployments & Links card on client detail has to
render raw UUIDs ("019da6b9-c056-796c-…") because the payload can't
say "this deployment is on `dev-hvds`". Frontend currently fetches
`/api/agents` separately and joins client-side — works, but is an
extra request per detail load.

**Expected:** add `node_name` (and optionally the agent's last-seen
IP) alongside `agent_id` in `clientDeploymentResponse`. One field,
no schema breakage.

**Scope:** backend. Frontend can drop the side `/api/agents` fetch
once the field lands.

## (future entries — keep the format consistent)

- Date / where / symptom / expected / scope
