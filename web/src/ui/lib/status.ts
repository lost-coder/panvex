import type { Status } from "@/ui/tokens/colors";

type BadgeVariant = Status | "default";

/** Agent presence state → severity */
export const presenceSeverity: Record<string, Status> = {
  online: "ok",
  degraded: "warn",
  offline: "error",
};

/** Enrollment token status → badge variant */
export const tokenStatusVariant: Record<string, BadgeVariant> = {
  active: "ok",
  consumed: "default",
  expired: "warn",
  revoked: "error",
};

/** User role → badge variant */
export const roleVariant: Record<string, BadgeVariant> = {
  admin: "ok",
  operator: "warn",
  viewer: "default",
};

/**
 * Deploy / health status → badge variant.
 *
 * The real per-deployment status vocabulary emitted by the backend
 * (`internal/controlplane/clients/types.go`) is `queued` / `succeeded` /
 * `failed` / `awaiting_node`. The badge row is meant to read as a
 * coherent escalation:
 *
 *  - `succeeded` → ok (green): the deployment applied cleanly.
 *  - `failed`    → error (red): Telemt rejected the config — needs
 *    operator attention now.
 *  - `awaiting_node` → warn (amber): the job expired before an offline
 *    node confirmed it. The reconciler will re-send on reconnect, so
 *    this is a *distinct* attention state from `failed` (nothing to
 *    fix, just waiting) but must not look the same as a freshly-queued
 *    job either.
 *  - `queued` is deliberately left OUT of this map (falls through to
 *    `"default"`, neutral): a job that's simply in flight is the
 *    normal, non-attention state — it should not compete visually with
 *    `awaiting_node`'s amber "waiting on an offline node" signal.
 *
 * The `ok`/`pending`/`error` keys below predate the real vocabulary and
 * are unreachable from the one call site (`DeployLinksCard.tsx` passes
 * the raw backend status straight through) — kept in case something
 * else starts passing those literal values, but they are not part of
 * the deploy-status escalation above.
 */
const _deployVariant: Record<string, BadgeVariant> = {
  ok: "ok",
  pending: "warn",
  error: "error",
  succeeded: "ok",
  failed: "error",
  awaiting_node: "warn",
};
export function deployVariant(status: string): BadgeVariant {
  return _deployVariant[status] ?? "default";
}

/**
 * Numeric coverage percentage → severity token. Single source of the
 * coverage thresholds (< 70 error, < 100 warn, full ok); both the color
 * class and any status-enum consumer derive from this.
 */
export function coverageStatus(pct: number): Status {
  if (pct < 70) return "error";
  if (pct < 100) return "warn";
  return "ok";
}

/**
 * Status severity → background-fill class (solid dot / beacon body).
 * Single source for the dot color maps the status primitives used to each
 * declare inline.
 */
export const statusBgClass: Record<Status, string> = {
  ok: "bg-status-ok",
  warn: "bg-status-warn",
  error: "bg-status-error",
};

/** Status severity → text/foreground color class. */
export const statusTextClass: Record<Status, string> = {
  ok: "text-status-ok",
  warn: "text-status-warn",
  error: "text-status-error",
};

/** Numeric coverage percentage → status color class. */
export function coverageColor(pct: number): string {
  const s = coverageStatus(pct);
  if (s === "error") return "text-status-error";
  if (s === "warn") return "text-status-warn";
  return "text-status-ok";
}
