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

/** Numeric coverage percentage → status color class */
export function coverageColor(pct: number): string {
  if (pct < 70) return "text-status-error";
  if (pct < 100) return "text-status-warn";
  return "text-status-ok";
}
