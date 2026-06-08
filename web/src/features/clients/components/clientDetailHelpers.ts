// R-Q-08: expiry helpers used by ClientDetailHero. The old 3-state
// `clientStatus`/`isExpired` were removed in Plan 2h — status now flows
// through the unified `deriveClientState` (ClientsPageCells).

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
