import { memo } from "react";
import { cn } from "@/ui/lib/cn";

export type PulseTone = "ok" | "warn" | "error" | "default";

export interface PulseTick {
  label: string;
  value: string;
  hint?: string | undefined;
  tone?: PulseTone | undefined;
  /** Optional 0–100 progress bar under the value. Good for usage/quota ticks. */
  barPct?: number | undefined;
  /** Optional secondary number rendered with an arrow (Δ +12 / -3). */
  delta?: string | undefined;
}

export interface PulseRowProps {
  ticks: PulseTick[];
  /** Optional className for the outer section. */
  className?: string | undefined;
}

const toneClass: Record<PulseTone, string> = {
  default: "text-fg",
  ok: "text-status-ok",
  warn: "text-status-warn",
  error: "text-status-error",
};

const barClass: Record<PulseTone, string> = {
  default: "bg-fg-muted",
  ok: "bg-status-ok",
  warn: "bg-status-warn",
  error: "bg-status-error",
};

function Tick({ label, value, hint, tone, barPct, delta }: Readonly<PulseTick>) {
  const t = tone ?? "default";
  return (
    <div className="flex flex-col gap-1 min-w-0">
      <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">
        {label}
      </span>
      <div className="flex items-baseline gap-2 min-w-0">
        <span
          className={cn(
            "text-2xl font-mono font-semibold leading-none tracking-tight tabular-nums",
            toneClass[t],
          )}
        >
          {value}
        </span>
        {delta && (
          <span
            className={cn(
              "text-[11px] font-mono tabular-nums",
              delta.startsWith("-") ? "text-status-error" : "text-status-ok",
            )}
          >
            {delta}
          </span>
        )}
      </div>
      {barPct !== undefined && (
        <div className="h-1 w-full rounded-full bg-border overflow-hidden mt-1">
          <div
            className={cn("h-full rounded-full transition-[width]", barClass[t])}
            style={{ width: `${Math.max(0, Math.min(100, barPct))}%` }}
          />
        </div>
      )}
      {hint && <span className="text-[10px] font-mono text-fg-muted truncate">{hint}</span>}
    </div>
  );
}

/**
 * Phase-7 pulse row: 2×N grid (2 on mobile, 4 on md+) with per-cell dividers.
 * Each tick shows one label / large value / optional hint with status tone.
 * Optional `barPct` shows a slim progress bar under the value (for
 * usage/quota), and `delta` renders a coloured Δ next to the value.
 */
function PulseRowImpl({ ticks, className }: Readonly<PulseRowProps>) {
  const cols = ticks.length;
  return (
    <section
      className={cn(
        "rounded-xs bg-bg-card border border-border grid grid-cols-2",
        cols === 3 ? "md:grid-cols-3" : "md:grid-cols-4",
        className,
      )}
    >
      {ticks.map((t, i) => {
        const isMobileSecondCol = i % 2 === 1;
        const isMobileSecondRow = i >= 2;
        return (
          <div
            key={t.label}
            className={cn(
              "min-w-0 p-4",
              isMobileSecondCol && "border-l border-divider",
              isMobileSecondRow && "border-t border-divider md:border-t-0",
              i > 0 && "md:border-l md:border-divider",
            )}
          >
            <Tick {...t} />
          </div>
        );
      })}
    </section>
  );
}

// Hot path: re-rendered on every WebSocket telemetry tick. Wrap in
// memo so unrelated parent re-renders (route loader, header clock)
// do not pay the layout cost. Callers should pass a memoised `ticks`
// array — Object.is on a freshly-built array is always !==.
export const PulseRow = memo(PulseRowImpl);
