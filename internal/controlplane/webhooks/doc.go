// Package webhooks implements at-least-once outbound HTTP delivery of
// control-plane events (agent.unhealthy, audit.security, job.failed,
// …) to operator-configured receivers (Slack, PagerDuty, custom).
//
// The package is the producer + worker side of an outbox pattern:
//
//	event source ─Publish▶ Producer ─INSERT▶ webhook_outbox ─claim▶ Worker ─POST▶ receiver
//
// Persistence is provided by an injected Storage; the in-memory
// implementation in this package is the test seam, real backends
// (postgres, sqlite) live in internal/controlplane/storage/{postgres,
// sqlite}/webhooks.go.
//
// Wire format. Each delivery is a POST with body = the event's
// canonical JSON payload, plus headers:
//
//	Content-Type: application/json
//	X-Panvex-Event: <action>           // e.g. "agent.unhealthy"
//	X-Panvex-Delivery: <outbox-id>     // idempotency key the receiver may dedupe by
//	X-Panvex-Timestamp: <RFC3339Nano>  // included in the HMAC, see below
//	X-Panvex-Signature: sha256=<hex>   // HMAC-SHA256(secret, timestamp + "\n" + body)
//
// The timestamp is part of the signed input so a receiver can reject
// replays (recommended window: ±5 minutes). The signature header
// format follows the de-facto convention used by GitHub / Stripe so
// existing receiver libraries verify without custom glue.
//
// Retry policy. On any non-2xx or transport error the worker
// increments attempt, records last_error, and reschedules at
// now() + backoff(attempt) (default exponential, capped at 1h).
// After MaxAttempts the row is dead-lettered (dead=1) and the
// worker emits a "webhook.dead_letter" event of its own (the audit
// trail is the durable record; see docs/superpowers/plans/2026-05-08-webhook-outbox.md).
//
// Security. SSRF-blocking is enforced by the worker before each
// outbound dial: receivers on private CIDRs (RFC1918, loopback,
// link-local, multicast) are refused unless the endpoint has
// allow_private=true. URLs must be https unless
// PANVEX_ALLOW_INSECURE_WEBHOOK is set.
package webhooks
