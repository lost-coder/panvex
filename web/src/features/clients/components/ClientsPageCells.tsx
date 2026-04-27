// R-Q-08: cell renderers + status helpers extracted from ClientsPage.tsx
// so the page container can stay focused on data orchestration. Each
// helper is pure: it accepts the snapshot inputs from props and returns
// the cell markup, no hooks.
//
// R-Q-24: helpers (effectiveClientStatus, isClientExpired) ship next to
// the cell components by design — splitting them into a dedicated file
// would force ClientsPage to learn two import paths for the same domain.
/* eslint-disable react-refresh/only-export-components */

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
  const map = {
    active: { label: "Active", variant: "ok" as const },
    disabled: { label: "Disabled", variant: "default" as const },
    expired: { label: "Expired", variant: "error" as const },
  };
  const { label, variant } = map[status];
  return <Badge variant={variant}>{label}</Badge>;
}

export function ClientTrafficCell({ used, quota }: Readonly<{ used: number; quota: number }>) {
  // No quota → just show used bytes; with a quota render a slim
  // progress bar + "used / quota" so operators see headroom at a glance.
  if (!quota) {
    return <MonoValue className="text-fg">{formatBytes(used)}</MonoValue>;
  }
  const pct = Math.min(100, (used / quota) * 100);
  const tone =
    pct >= 100 ? "bg-status-error" : pct >= 80 ? "bg-status-warn" : "bg-status-ok";
  return (
    <div className="flex flex-col gap-1 min-w-[120px]">
      <span className="text-[11px] font-mono text-fg tabular-nums">
        {formatBytes(used)}
        <span className="text-fg-muted"> / {formatQuota(quota)}</span>
      </span>
      <div className="h-1 w-full rounded-full bg-border overflow-hidden">
        <div className={cn("h-full rounded-full", tone)} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

export function ClientExpiryCell({ rfc, nowSec }: Readonly<{ rfc: string; nowSec: number }>) {
  if (!rfc) return <span className="text-[11px] font-mono text-fg-muted">Never</span>;
  const t = Date.parse(rfc);
  if (!Number.isFinite(t)) return <span className="text-[11px] font-mono text-fg-muted">—</span>;
  const days = Math.floor((t / 1000 - nowSec) / 86_400);
  const tone =
    days < 0 ? "text-status-error" : days < 7 ? "text-status-warn" : "text-fg-muted";
  const subtitle = days < 0 ? `${Math.abs(days)}d ago` : days === 0 ? "today" : `in ${days}d`;
  return (
    <div className="flex flex-col">
      <span className="text-[11px] font-mono text-fg tabular-nums">{formatExpiry(rfc)}</span>
      <span className={cn("text-[10px] font-mono", tone)}>{subtitle}</span>
    </div>
  );
}
