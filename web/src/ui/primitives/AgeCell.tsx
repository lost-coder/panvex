import { cn } from "@/ui/lib/cn";
import { formatAge, formatTime } from "@/ui/lib/format";

export interface AgeCellProps {
  /** Unix seconds. Takes precedence over `rfc3339`. */
  unixSec?: number;
  /** RFC3339 timestamp — converted to unix seconds internally. */
  rfc3339?: string;
  /**
   * Rendering mode:
   * - `age` (default): "5m ago" on top, absolute time below.
   * - `expires`: absolute time on top, countdown below (coloured).
   */
  mode?: "age" | "expires";
  /** Snapshot of current unix seconds. Required for `expires` mode. */
  nowSec?: number;
  /** Override alignment. Defaults to "right". */
  align?: "right" | "left";
  className?: string;
}

function toUnix(props: Pick<AgeCellProps, "unixSec" | "rfc3339">): number | undefined {
  if (props.unixSec !== undefined) return props.unixSec;
  if (!props.rfc3339) return undefined;
  const t = Date.parse(props.rfc3339);
  return Number.isFinite(t) ? Math.floor(t / 1000) : undefined;
}

// "in 2h 15m" / "5s" / "expired 1m ago". Same vocabulary as TokenList used.
function countdown(seconds: number): string {
  const abs = Math.abs(seconds);
  let out: string;
  if (abs < 60) out = `${abs}s`;
  else if (abs < 3_600) out = `${Math.floor(abs / 60)}m`;
  else if (abs < 86_400) {
    const h = Math.floor(abs / 3_600);
    const m = Math.floor((abs % 3_600) / 60);
    out = m === 0 ? `${h}h` : `${h}h ${m}m`;
  } else {
    const d = Math.floor(abs / 86_400);
    const h = Math.floor((abs % 86_400) / 3_600);
    out = h === 0 ? `${d}d` : `${d}d ${h}h`;
  }
  return seconds >= 0 ? `in ${out}` : `${out} ago`;
}

/**
 * Two-line time cell: absolute time + relative hint. Used on list pages so
 * operators see both "when exactly" and "how long ago / how long left" in
 * one glance.
 */
export function AgeCell({
  unixSec,
  rfc3339,
  mode = "age",
  nowSec,
  align = "right",
  className,
}: AgeCellProps) {
  const epoch = toUnix({ unixSec, rfc3339 });
  if (epoch === undefined) {
    return (
      <span className={cn("text-[11px] font-mono text-fg-muted", className)}>—</span>
    );
  }

  if (mode === "expires") {
    // Countdown tone: red if past, amber if <5 min, green otherwise.
    const remaining = nowSec !== undefined ? epoch - nowSec : undefined;
    let toneClass = "text-fg-muted";
    if (remaining !== undefined) {
      if (remaining <= 0) toneClass = "text-status-error";
      else if (remaining < 300) toneClass = "text-status-warn";
      else toneClass = "text-status-ok";
    }
    return (
      <div
        className={cn(
          "flex flex-col",
          align === "right" ? "text-right" : "text-left",
          className,
        )}
      >
        <span className="text-[11px] font-mono text-fg tabular-nums">
          {formatTime(epoch)}
        </span>
        {remaining !== undefined && (
          <span className={cn("text-[10px] font-mono tabular-nums", toneClass)}>
            {countdown(remaining)}
          </span>
        )}
      </div>
    );
  }

  // Default: age + absolute.
  return (
    <div
      className={cn(
        "flex flex-col",
        align === "right" ? "text-right" : "text-left",
        className,
      )}
    >
      <span className="text-[11px] font-mono text-fg tabular-nums">
        {formatAge(epoch)}
      </span>
      <span className="text-[10px] font-mono text-fg-muted">{formatTime(epoch)}</span>
    </div>
  );
}
