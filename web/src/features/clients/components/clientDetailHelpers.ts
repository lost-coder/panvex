// R-Q-08: status / expiry helpers extracted from ClientDetailPage.tsx
// so the host page is left with composition-only concerns.

export function isExpired(rfc: string): boolean {
  if (!rfc) return false;
  const t = Date.parse(rfc);
  return Number.isFinite(t) && t < Date.now();
}

export function clientStatus(
  enabled: boolean,
  rfc: string,
): "active" | "disabled" | "expired" {
  if (isExpired(rfc)) return "expired";
  return enabled ? "active" : "disabled";
}

export function expiresSuffix(rfc: string): string {
  if (!rfc) return "never";
  const t = Date.parse(rfc);
  if (!Number.isFinite(t)) return "—";
  const days = Math.floor((t - Date.now()) / (1000 * 60 * 60 * 24));
  if (days < 0) return `${Math.abs(days)}d ago`;
  if (days === 0) return "today";
  return `in ${days}d`;
}

export function expiresTone(rfc: string): "default" | "warn" | "error" {
  if (!rfc) return "default";
  const t = Date.parse(rfc);
  if (!Number.isFinite(t)) return "default";
  const days = Math.floor((t - Date.now()) / (1000 * 60 * 60 * 24));
  if (days < 0) return "error";
  if (days < 7) return "warn";
  return "default";
}
