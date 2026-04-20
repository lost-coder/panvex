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

## (future entries — keep the format consistent)

- Date / where / symptom / expected / scope
