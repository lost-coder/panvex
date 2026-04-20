// src/primitives/InitCard.tsx
import { cn } from "@/ui/lib/cn";
import { ProgressBar } from "./ProgressBar";
import type { InitCardProps } from "@/shared/api/types-pages/pages";

export function InitCard({
  stage,
  progressPct,
  attempt,
  retryLimit,
  elapsedSecs,
  lastError,
  degraded: _degraded,
}: InitCardProps) {
  const borderColor = lastError ? "border-status-error/30" : "border-status-warn/30";
  const bgColor = lastError ? "bg-status-error/5" : "bg-status-warn/5";

  return (
    <div className={cn("rounded-xs border p-4 flex flex-col gap-3", borderColor, bgColor)}>
      <div className="flex items-center justify-between">
        <span
          className={cn(
            "text-sm font-semibold",
            lastError ? "text-status-error" : "text-status-warn",
          )}
        >
          {lastError ? "Initialization Error" : "Initializing Telemt runtime..."}
        </span>
        <span className="text-xs font-mono text-fg-muted">
          Stage {attempt}/{retryLimit} · {Math.round(progressPct)}%
        </span>
      </div>

      <ProgressBar value={Math.round(progressPct)} max={100} showValue={false} className="h-1.5" />

      <div className="grid grid-cols-3 gap-2 text-xs">
        <div>
          <span className="text-fg-muted">Stage:</span>{" "}
          <span className="font-mono text-fg">{stage}</span>
        </div>
        <div>
          <span className="text-fg-muted">Attempt:</span>{" "}
          <span className="font-mono text-fg">
            {attempt} / {retryLimit}
          </span>
        </div>
        <div>
          <span className="text-fg-muted">Elapsed:</span>{" "}
          <span className="font-mono text-fg">{elapsedSecs}s</span>
        </div>
      </div>

      {lastError && (
        <div className="text-xs text-status-error font-mono bg-status-error/5 rounded px-2 py-1.5">
          {lastError}
        </div>
      )}
    </div>
  );
}
