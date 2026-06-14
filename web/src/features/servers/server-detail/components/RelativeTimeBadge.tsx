import { memo } from "react";

import { cn } from "@/ui";

/**
 * Spinning-arrow chip that shows when the page last refreshed. Tone
 * flips to warn when the data has been stale beyond the freshness
 * threshold (parent hook decides via `stale`).
 *
 * Memoised — primitive props (`label`, `stale`) make this a perfect
 * memo candidate; it ticks every second while the rest of the page
 * shouldn't repaint.
 */
export const RelativeTimeBadge = memo(_RelativeTimeBadge);

function _RelativeTimeBadge({ label, stale }: { label: string; stale: boolean }) {
  return (
    <span
      className={cn(
        "text-nano font-mono tabular-nums inline-flex items-center gap-1 rounded-full px-2 py-0.5 border transition-colors duration-500",
        stale
          ? "bg-status-warn/10 border-status-warn/15 text-status-warn"
          : "bg-status-ok/10 border-status-ok/15 text-fg-muted",
      )}
    >
      <span className="text-micro animate-spin" style={{ animationDuration: "3s" }}>
        ↻
      </span>
      {label}
    </span>
  );
}
