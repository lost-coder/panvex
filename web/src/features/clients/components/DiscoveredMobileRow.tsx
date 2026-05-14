// R-Q-08: mobile compact row + small pulse-cell extracted from
// DiscoveredClientsPage.tsx.

import { useTranslation } from "react-i18next";

import { Button, cn, formatBytes } from "@/ui";
import type { DiscoveredGroup } from "@/features/clients/lib/groupDiscovered";

import { DiscoveredStatusPill } from "./DiscoveredColumns";

export interface DiscoveredMobileRowProps {
  row: DiscoveredGroup;
  selected: boolean;
  onToggleSelect: (key: string) => void;
  onAdopt?: ((ids: string[]) => void) | undefined;
  onIgnore?: ((ids: string[]) => void) | undefined;
  busy?: boolean | undefined;
}

export function DiscoveredMobileRow({
  row,
  selected,
  onToggleSelect,
  onAdopt,
  onIgnore,
  busy,
}: Readonly<DiscoveredMobileRowProps>) {
  const { t } = useTranslation("clients");
  const interactive = row.status === "pending_review";
  return (
    <div className="flex flex-col gap-2 px-4 py-3 border-b border-divider">
      <div className="flex items-center gap-3">
        {interactive && (
          <input
            type="checkbox"
            aria-label={t("discovered.table.selectOne", { name: row.clientName })}
            checked={selected}
            onChange={() => onToggleSelect(row.key)}
            className="accent-accent size-4 cursor-pointer"
          />
        )}
        <span className="font-medium text-fg truncate flex-1">{row.clientName}</span>
        <DiscoveredStatusPill status={row.status} />
      </div>
      <div className="flex flex-wrap gap-1 pl-7">
        {row.discoveredOn.map((n) => (
          <span
            key={n}
            className="font-mono text-[10px] text-fg-muted px-1.5 py-0.5 rounded-xs border border-divider bg-bg"
          >
            {n}
          </span>
        ))}
      </div>
      <div className="flex items-center justify-between pl-7 text-[11px] font-mono text-fg-muted">
        <span>
          {row.currentConnections} {t("table.connsSuffix")} · {row.activeUniqueIps}{" "}
          {t("table.ipsSuffix")} · {formatBytes(row.totalOctets)}
        </span>
        {Number.isFinite(row.discoveredAtUnix) && row.discoveredAtUnix > 0 && (
          <span>{new Date(row.discoveredAtUnix * 1000).toLocaleString()}</span>
        )}
      </div>
      {interactive && (
        <div className="flex gap-2 pl-7">
          <Button size="sm" disabled={busy} onClick={() => onAdopt?.(row.ids)}>
            {t("discovered.table.adopt")}
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => onIgnore?.(row.ids)}>
            {t("discovered.table.ignore")}
          </Button>
        </div>
      )}
    </div>
  );
}

export interface DiscoveredPulseCellProps {
  i: number;
  label: string;
  value: number;
  tone?: "default" | "ok" | "warn" | "error";
}

export function DiscoveredPulseCell({ i, label, value, tone }: Readonly<DiscoveredPulseCellProps>) {
  const isSecondCol = i % 2 === 1;
  const isSecondRow = i >= 2;
  const toneClass: Record<NonNullable<typeof tone>, string> = {
    default: "text-fg",
    ok: "text-status-ok",
    warn: "text-status-warn",
    error: "text-status-error",
  };
  return (
    <div
      className={cn(
        "min-w-0 p-4",
        isSecondCol && "border-l border-divider",
        isSecondRow && "border-t border-divider md:border-t-0",
        i > 0 && "md:border-l md:border-divider",
      )}
    >
      <div className="flex flex-col gap-1 min-w-0">
        <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">{label}</span>
        <span
          className={cn(
            "text-2xl font-mono font-semibold leading-none tracking-tight tabular-nums",
            toneClass[tone ?? "default"],
          )}
        >
          {value.toLocaleString()}
        </span>
      </div>
    </div>
  );
}
