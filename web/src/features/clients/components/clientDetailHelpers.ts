// R-Q-08: expiry helpers used by ClientDetailHero. The old 3-state
// `clientStatus`/`isExpired` were removed in Plan 2h — status now flows
// through the unified `deriveClientState` (ClientsPageCells).

import i18next from "i18next";

// Relative expiry label. Localised via i18next (the strings used to be
// hardcoded English — "never"/"today"/"in 5d" — and leaked into the RU UI).
export function expiresSuffix(rfc: string): string {
  const tr = (key: string, opts?: Record<string, unknown>): string =>
    i18next.t(`clients:detail.expiresRelative.${key}`, opts ?? {}) as unknown as string;
  if (!rfc) return tr("never");
  const t = Date.parse(rfc);
  if (!Number.isFinite(t)) return "—";
  const days = Math.floor((t - Date.now()) / (1000 * 60 * 60 * 24));
  if (days < 0) return tr("daysAgo", { days: Math.abs(days) });
  if (days === 0) return tr("today");
  return tr("inDays", { days });
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
