import { useNowSec } from "@/shared/hooks/useNowSec";

/**
 * Compact "time since last telemetry" label for the server-detail pulse bar,
 * plus a staleness flag the bar renders as a warning dot.
 *
 * Deliberately NOT the shared `useRelativeTime` from `@/shared/hooks`: that
 * one produces localized prose ("5m ago") on a 30s cadence, which is right for
 * table cells and wrong here — the pulse bar is a liveness indicator, so it
 * needs the terse form and a per-second tick. What it does share is the clock:
 * useNowSec(1000) instead of another hand-rolled setInterval.
 */
export function useRelativeTime(date: Date | undefined): { label: string; stale: boolean } {
  const nowSec = useNowSec(1000);

  if (!date) return { label: "", stale: false };

  const secs = Math.round(nowSec - date.getTime() / 1000);
  const label = (() => {
    if (secs < 2) return "now";
    if (secs < 60) return `${secs}s`;
    return `${Math.floor(secs / 60)}m`;
  })();
  return { label, stale: secs > 10 };
}
