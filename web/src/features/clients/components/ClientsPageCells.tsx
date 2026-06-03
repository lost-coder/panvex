// R-Q-08: cell renderers + status helpers extracted from ClientsPage.tsx
// so the page container can stay focused on data orchestration. Each
// helper is pure: it accepts the snapshot inputs from props and returns
// the cell markup, no hooks.
//
// R-Q-24: helpers (effectiveClientStatus, isClientExpired) ship next to
// the cell components by design — splitting them into a dedicated file
// would force ClientsPage to learn two import paths for the same domain.
/* eslint-disable react-refresh/only-export-components */

import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";

import {
  Badge,
  MonoValue,
  cn,
  formatBytes,
  formatExpiry,
  formatQuota,
  type ClientListItem,
} from "@/ui";

export type EffectiveClientStatus = "active" | "disabled" | "expired";

export function isClientExpired(expirationRfc3339: string, nowMs: number): boolean {
  if (!expirationRfc3339) return false;
  const t = Date.parse(expirationRfc3339);
  return Number.isFinite(t) && t < nowMs;
}

export function effectiveClientStatus(
  c: ClientListItem,
  nowMs: number,
): EffectiveClientStatus {
  if (isClientExpired(c.expirationRfc3339, nowMs)) return "expired";
  return c.enabled ? "active" : "disabled";
}

export function ClientStatusBadge({ status }: Readonly<{ status: EffectiveClientStatus }>) {
  const { t } = useTranslation("clients");
  const map = {
    active: { label: t("statusBadge.active"), variant: "ok" as const },
    disabled: { label: t("statusBadge.disabled"), variant: "default" as const },
    expired: { label: t("statusBadge.expired"), variant: "error" as const },
  };
  const { label, variant } = map[status];
  return <Badge variant={variant}>{label}</Badge>;
}

// `quota` is the per-Telemt-node value the operator entered. Each
// agent enforces it independently, so the *fleet-wide* quota is
// `quota × nodes`. We compare summed traffic against that effective
// number — otherwise the bar saturates at 100% the moment the first
// node spends its slice while the rest still have headroom.
export function ClientTrafficCell({ used, quota, nodes }: Readonly<{ used: number; quota: number; nodes: number }>) {
  if (!quota) {
    return <MonoValue className="text-fg">{formatBytes(used)}</MonoValue>;
  }
  const denom = quota * Math.max(1, nodes);
  const pct = Math.min(100, (used / denom) * 100);
  const tone = (() => {
    if (pct >= 100) return "bg-status-error";
    if (pct >= 80) return "bg-status-warn";
    return "bg-status-ok";
  })();
  return (
    <div className="flex flex-col gap-1 min-w-[120px]">
      <span className="text-micro font-mono text-fg tabular-nums">
        {formatBytes(used)}
        <span className="text-fg-muted"> / {formatQuota(denom)}</span>
      </span>
      <div className="h-1 w-full rounded-full bg-border overflow-hidden">
        <div className={cn("h-full rounded-full", tone)} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

export function ClientExpiryCell({
  rfc,
  nowSec,
  t,
}: Readonly<{ rfc: string; nowSec: number; t: TFunction<"clients"> }>) {
  if (!rfc) return <span className="text-micro font-mono text-fg-muted">{t("expiry.never")}</span>;
  const parsed = Date.parse(rfc);
  if (!Number.isFinite(parsed))
    return <span className="text-micro font-mono text-fg-muted">{t("expiry.unknown")}</span>;
  const days = Math.floor((parsed / 1000 - nowSec) / 86_400);
  const tone = (() => {
    if (days < 0) return "text-status-error";
    if (days < 7) return "text-status-warn";
    return "text-fg-muted";
  })();
  const subtitle = (() => {
    if (days < 0) return t("expiry.agoDays", { count: Math.abs(days) });
    if (days === 0) return t("expiry.today");
    return t("expiry.inDays", { count: days });
  })();
  return (
    <div className="flex flex-col">
      <span className="text-micro font-mono text-fg tabular-nums">{formatExpiry(rfc)}</span>
      <span className={cn("text-nano font-mono", tone)}>{subtitle}</span>
    </div>
  );
}
