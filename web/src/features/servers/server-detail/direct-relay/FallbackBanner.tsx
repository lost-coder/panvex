import { useEffect, useState } from "react";

import { cn } from "@/ui/lib/cn";

export interface FallbackBannerProps {
  durationSeconds: number;
  escalated: boolean;
  enteredAtUnix?: number | null;
}

const TICK_MS = 30_000;

export function FallbackBanner({
  durationSeconds,
  escalated,
  enteredAtUnix = null,
}: Readonly<FallbackBannerProps>) {
  const [liveDurationSeconds, setLiveDurationSeconds] =
    useState(durationSeconds);

  useEffect(() => {
    setLiveDurationSeconds(durationSeconds);
    if (enteredAtUnix == null) return;
    const id = window.setInterval(() => {
      setLiveDurationSeconds(
        Math.max(0, Math.floor(Date.now() / 1000) - enteredAtUnix),
      );
    }, TICK_MS);
    return () => window.clearInterval(id);
  }, [durationSeconds, enteredAtUnix]);

  const severity = escalated ? "critical" : "warn";
  const headline = escalated
    ? "ME pool down — fallback active"
    : "Running on direct fallback";
  const body = escalated
    ? "ME pool has been unavailable for over 30 minutes. Investigate ME diagnostics."
    : "ME pool currently unavailable; traffic flowing via direct fallback.";

  return (
    <div
      data-severity={severity}
      className={cn(
        "rounded-md border p-3 text-sm",
        escalated
          ? "bg-status-error-soft border-status-error text-status-error"
          : "bg-status-warning-soft border-status-warning text-status-warning",
      )}
    >
      <strong>{headline}</strong>
      <p className="mt-1 text-fg">
        {body} Active for {Math.round(liveDurationSeconds / 60)} min.
      </p>
    </div>
  );
}
