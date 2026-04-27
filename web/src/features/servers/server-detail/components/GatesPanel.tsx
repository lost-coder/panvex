// R-Q-24: see DcTiles.tsx — same internal/exported memo pattern.
/* eslint-disable react-refresh/only-export-components */
import { memo } from "react";

import { cn } from "@/ui";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

// ─── Gates panel ──────────────────────────────────────────────────────
function GateRow({
  label,
  on,
  alertWhenOn,
  neutralWhenOn,
}: Readonly<{
  label: string;
  on: boolean;
  alertWhenOn?: boolean;
  neutralWhenOn?: boolean;
}>) {
  const tone: "ok" | "warn" | "error" | "default" = alertWhenOn
    ? on
      ? "warn"
      : "ok"
    : neutralWhenOn
      ? "default"
      : on
        ? "ok"
        : "error";
  const dot = (() => {
    if (tone === "ok") return "bg-status-ok";
    if (tone === "warn") return "bg-status-warn";
    if (tone === "error") return "bg-status-error";
    return "bg-fg-muted/60";
  })();
  // Handoff-style "dashed divider row" inside the Gates column — lighter
  // than the solid dividers used in the Upstreams column so the two
  // columns read as different content types at a glance.
  return (
    <div className="flex items-center justify-between gap-3 py-2 border-b border-dashed border-divider last:border-b-0">
      <div className="flex items-center gap-2 min-w-0">
        <span className={cn("h-1.5 w-1.5 rounded-full shrink-0", dot)} />
        <span className="text-xs text-fg truncate">{label}</span>
      </div>
      <span className="text-[10px] font-mono font-semibold uppercase tracking-wider text-fg-muted">
        {on ? "on" : "off"}
      </span>
    </div>
  );
}

/**
 * Memoised — `gates` is a slice of the parent `server` object, which
 * is referentially stable across re-renders that don't change the
 * server payload, so the rows don't re-render needlessly.
 */
export const GatesPanel = memo(_GatesPanel);

function _GatesPanel({ gates }: Readonly<{ gates: ServerDetailPageProps["server"]["gates"] }>) {
  return (
    <div className="flex flex-col">
      <GateRow label="Accepting connections" on={gates.acceptingNewConnections} />
      <GateRow label="ME runtime ready" on={gates.meRuntimeReady} />
      <GateRow label="Middle proxy" on={gates.useMiddleProxy} neutralWhenOn />
      <GateRow label="ME → DC fallback" on={gates.me2dcFallbackEnabled} neutralWhenOn />
      <GateRow label="Reroute active" on={gates.rerouteActive} alertWhenOn />
      <GateRow label="Read-only mode" on={gates.readOnly} alertWhenOn />
      <GateRow label="Degraded" on={gates.degraded} alertWhenOn />
    </div>
  );
}
