import { useEffect, useRef, useState } from "react";

// Stage 5 ("panel in pocket"): native-feeling pull-to-refresh for touch
// devices. The whole panel scrolls on the window, so we listen there and
// only engage when the page is already scrolled to the very top and the
// finger drags downward — that keeps normal scrolling and bottom-sheet
// drags untouched. Desktop / mouse never triggers it.

const THRESHOLD = 70; // px of resolved pull needed to fire a refresh
const MAX_PULL = 96; // visual cap so the indicator doesn't run away
const RESISTANCE = 0.5; // dampen the drag so it feels rubber-bandy

export interface PullToRefreshState {
  /** Current resolved pull distance in px (0 when idle). */
  pull: number;
  /** True while the onRefresh promise is in flight. */
  refreshing: boolean;
  /** True once the pull has passed the trigger threshold (for the hint). */
  armed: boolean;
}

export function usePullToRefresh(onRefresh: () => Promise<unknown> | void): PullToRefreshState {
  const [pull, setPull] = useState(0);
  const [refreshing, setRefreshing] = useState(false);
  // Keep the latest callback without re-binding listeners every render.
  const onRefreshRef = useRef(onRefresh);
  useEffect(() => {
    onRefreshRef.current = onRefresh;
  }, [onRefresh]);
  const refreshingRef = useRef(false);

  useEffect(() => {
    if (globalThis.window === undefined) return;
    // Touch-only affordance — skip on devices that can hover (desktop).
    if (globalThis.matchMedia?.("(hover: hover)").matches) return;

    let startY = 0;
    let active = false;

    const atTop = () =>
      (globalThis.scrollY || document.documentElement.scrollTop || 0) <= 0;

    const onStart = (e: TouchEvent) => {
      if (refreshingRef.current || e.touches.length !== 1 || !atTop()) {
        active = false;
        return;
      }
      startY = e.touches[0]!.clientY;
      active = true;
    };

    const onMove = (e: TouchEvent) => {
      if (!active || refreshingRef.current) return;
      const delta = e.touches[0]!.clientY - startY;
      // Only react to downward drags that start at the top; if the user
      // scrolls back up or the page left the top, bail out cleanly.
      if (delta <= 0 || !atTop()) {
        if (pull !== 0) setPull(0);
        active = delta > 0 && atTop() ? active : false;
        return;
      }
      const resolved = Math.min(delta * RESISTANCE, MAX_PULL);
      // Suppress the browser's native overscroll/rubber-band so our
      // indicator is the only thing that moves.
      if (e.cancelable) e.preventDefault();
      setPull(resolved);
    };

    const onEnd = () => {
      if (!active) return;
      active = false;
      setPull((current) => {
        if (current >= THRESHOLD && !refreshingRef.current) {
          refreshingRef.current = true;
          setRefreshing(true);
          void Promise.resolve(onRefreshRef.current())
            .finally(() => {
              refreshingRef.current = false;
              setRefreshing(false);
              setPull(0);
            });
          return THRESHOLD; // hold the indicator while refreshing
        }
        return 0;
      });
    };

    // touchmove must be non-passive so preventDefault() can cancel overscroll.
    globalThis.addEventListener("touchstart", onStart, { passive: true });
    globalThis.addEventListener("touchmove", onMove, { passive: false });
    globalThis.addEventListener("touchend", onEnd, { passive: true });
    globalThis.addEventListener("touchcancel", onEnd, { passive: true });
    return () => {
      globalThis.removeEventListener("touchstart", onStart);
      globalThis.removeEventListener("touchmove", onMove);
      globalThis.removeEventListener("touchend", onEnd);
      globalThis.removeEventListener("touchcancel", onEnd);
    };
    // pull is intentionally excluded — listeners read/write it via setState
    // updater form; re-binding on every pull frame would be wasteful.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return { pull, refreshing, armed: pull >= THRESHOLD };
}
