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

## (future entries — keep the format consistent)

- Date / where / symptom / expected / scope
