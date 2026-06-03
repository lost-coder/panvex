// R-Q-24: see DcTiles.tsx — same internal/exported memo pattern, plus
// a public PulseTickData type co-located with the component.
/* eslint-disable react-refresh/only-export-components */
import { memo } from "react";

import { cn, deltaClass, deltaArrow } from "@/ui";

// ─── Pulse row tick ──────────────────────────────────────────────────
// "Ticker-style" metric without a card: compact label, large mono value,
// optional delta/hint line beneath. Reads as a ribbon of signals when
// four of these are placed side-by-side.
export interface PulseTickData {
  label: string;
  value: string | number;
  unit?: string;
  /** Formatted delta like "+2.1%" — rendered next to an arrow glyph. */
  deltaLabel?: string;
  deltaDirection?: "up" | "down" | "flat";
  hint?: string;
  tone?: "ok" | "warn" | "error" | "default";
}

function PulseTick({
  label,
  value,
  unit,
  deltaLabel,
  deltaDirection,
  hint,
  tone,
  centered,
}: PulseTickData & { centered?: boolean }) {
  const toneClass: Record<NonNullable<typeof tone>, string> = {
    default: "text-fg",
    ok: "text-status-ok",
    warn: "text-status-warn",
    error: "text-status-error",
  };
  // Cell owns padding only; its wrapper places the dividers. Centered
  // variant is used inside the 2×2 mobile grid so each cell reads as a
  // single focal point.
  return (
    <div
      className={cn(
        "flex flex-col gap-1 px-3 py-3 md:px-5 md:py-0.5 min-w-0",
        centered && "items-center text-center",
      )}
    >
      <span className="text-nano font-mono uppercase tracking-wider text-fg-muted truncate">
        {label}
      </span>
      <div className={cn("flex items-baseline gap-1", toneClass[tone ?? "default"])}>
        <span className="text-2xl font-mono font-semibold leading-none tracking-tight tabular-nums">
          {value}
        </span>
        {unit && <span className="text-sm font-mono text-fg-muted">{unit}</span>}
      </div>
      {(deltaLabel || hint) && (
        <div
          className={cn(
            "flex items-center gap-2 text-nano font-mono",
            centered && "justify-center",
          )}
        >
          {deltaLabel && (
            <span className={cn(deltaClass(deltaDirection))}>
              {deltaArrow(deltaDirection)}{" "}
              {deltaLabel}
            </span>
          )}
          {hint && <span className="text-fg-muted truncate">{hint}</span>}
        </div>
      )}
    </div>
  );
}

/**
 * Pulse strip: 4×1 ribbon on desktop, 2×2 grid on mobile. Dividers are
 * drawn as inset pseudo-elements (`before` for the vertical seam,
 * `after` for the horizontal) so the lines don't run all the way to
 * the card's rounded border — matches the "fine UI" feel of the
 * handoff reference. Mobile cells also centre their content.
 */
/**
 * Wrapped in `React.memo` because the parent passes a memoised `items`
 * array (via `useMemo`) and a stable `variant` literal — only data
 * changes should re-render the ribbon.
 */
export const PulseGrid = memo(_PulseGrid);

function _PulseGrid({
  variant,
  items,
}: {
  variant: "desktop" | "mobile";
  items: PulseTickData[];
}) {
  if (variant === "desktop") {
    return (
      <section className="rounded-xs bg-bg-card border border-border px-5 py-4 grid grid-cols-4 gap-0">
        {items.map((item, i) => (
          <div
            key={item.label}
            className={cn(
              "relative min-w-0",
              // inset vertical divider on every cell except the first
              i > 0 &&
                "before:content-[''] before:absolute before:left-0 before:top-2 before:bottom-2 before:w-px before:bg-divider",
            )}
          >
            <PulseTick {...item} />
          </div>
        ))}
      </section>
    );
  }
  return (
    <section className="rounded-xs bg-bg-card border border-border grid grid-cols-2">
      {items.map((item, i) => {
        const isSecondCol = i % 2 === 1;
        const isSecondRow = i >= 2;
        return (
          <div
            key={item.label}
            className={cn(
              "relative min-w-0",
              // inset vertical divider for right column cells
              isSecondCol &&
                "before:content-[''] before:absolute before:left-0 before:top-3 before:bottom-3 before:w-px before:bg-divider",
              // inset horizontal divider for bottom row cells
              isSecondRow &&
                "after:content-[''] after:absolute after:top-0 after:left-4 after:right-4 after:h-px after:bg-divider",
            )}
          >
            <PulseTick {...item} centered />
          </div>
        );
      })}
    </section>
  );
}
