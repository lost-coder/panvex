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

/** Deploy / health status → badge variant */
const _deployVariant: Record<string, BadgeVariant> = {
  ok: "ok",
  pending: "warn",
  error: "error",
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
