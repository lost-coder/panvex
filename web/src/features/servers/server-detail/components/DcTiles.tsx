// R-Q-24: file co-locates an internal sub-component (DcTile) with the
// memoised public component. Splitting them into separate files would
// cost more than the HMR fast-refresh benefit. Disable react-refresh
// on this file only.
/* eslint-disable react-refresh/only-export-components */
import { memo } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/ui";
import type { ServerDcData } from "@/shared/api/types-pages/pages";

function rttClass(rttMs: number): string {
  if (rttMs > 300) return "text-status-error";
  if (rttMs > 200) return "text-status-warn";
  return "text-fg";
}

// ─── 12-DC grid of tiles (problem-first ordering) ─────────────────────
function DcTile({ dc, onClick }: Readonly<{ dc: ServerDcData; onClick: () => void }>) {
  const { t } = useTranslation("servers");
  const status: "ok" | "warn" | "error" = (() => {
    if (dc.coveragePct < 70) return "error";
    if (dc.coveragePct < 100) return "warn";
    return "ok";
  })();
  const toneBorder = (() => {
    if (status === "error") return "border-status-error/50";
    if (status === "warn") return "border-status-warn/40";
    return "border-divider";
  })();
  const toneBar = (() => {
    if (status === "error") return "bg-status-error";
    if (status === "warn") return "bg-status-warn";
    return "bg-status-ok/80";
  })();
  const toneText = (() => {
    if (status === "error") return "text-status-error";
    if (status === "warn") return "text-status-warn";
    return "text-fg";
  })();
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "text-left rounded-xs bg-bg-card border px-3 py-2.5 flex flex-col gap-2 hover:bg-bg-hover transition-colors",
        toneBorder,
      )}
    >
      <div className="flex items-center justify-between">
        <span className="text-xs font-mono font-semibold text-fg">{`DC${dc.dc}`}</span>
        <span className={cn("h-1.5 w-1.5 rounded-full", toneBar)} />
      </div>
      <div className="flex items-baseline gap-1">
        <span className={cn("text-lg font-mono font-semibold tabular-nums", toneText)}>
          {dc.coveragePct}
        </span>
        <span className="text-xs font-mono text-fg-muted">%</span>
      </div>
      <div className="h-1 w-full rounded-full bg-border overflow-hidden">
        <div className={cn("h-full rounded-full", toneBar)} style={{ width: `${dc.coveragePct}%` }} />
      </div>
      <div className="flex items-center justify-between text-[10px] font-mono text-fg-muted">
        <span>
          {t("detail.dcTileLabels.writers")}{" "}
          <span className={cn(dc.aliveWriters < dc.requiredWriters ? "text-status-warn" : "text-fg")}>
            {dc.aliveWriters}/{dc.requiredWriters}
          </span>
        </span>
        <span>
          {t("detail.dcTileLabels.rtt")}{" "}
          <span className={cn(rttClass(dc.rttMs ?? 0))}>
            {dc.rttMs ?? "—"}
          </span>
        </span>
        <span>{t("detail.dcTileLabels.load", { value: Math.round(dc.load) })}</span>
      </div>
    </button>
  );
}

/**
 * Memoised — parent passes a memoised `sortedDcs` array and a stable
 * `useCallback` setter, so the tile grid only re-renders when the
 * underlying DC list changes.
 */
export const DcTiles = memo(_DcTiles);

function _DcTiles({
  dcs,
  onSelect,
}: Readonly<{
  dcs: ServerDcData[];
  onSelect: (dc: Readonly<ServerDcData>) => void;
}>) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-2">
      {dcs.map((dc) => (
        <DcTile key={dc.dc} dc={dc} onClick={() => onSelect(dc)} />
      ))}
    </div>
  );
}
