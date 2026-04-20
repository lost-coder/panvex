import { useEffect, useState } from "react";

/**
 * Self-refreshing snapshot of "now" in unix seconds. Refreshes on `intervalMs`
 * cadence (default 30s) so TTL countdowns and rolling-window filters don't
 * freeze at mount time.
 *
 * Why not just call `Date.now()` inline:
 * - Violates react-hooks/purity (impure function in render path).
 * - `useState(() => Date.now())` captures mount time and never updates;
 *   operators leave tabs open for hours and see stale windows.
 */
export function useNowSec(intervalMs = 30_000): number {
  const [nowSec, setNowSec] = useState(() => Math.floor(Date.now() / 1000));
  useEffect(() => {
    const id = setInterval(
      () => setNowSec(Math.floor(Date.now() / 1000)),
      intervalMs,
    );
    return () => clearInterval(id);
  }, [intervalMs]);
  return nowSec;
}
