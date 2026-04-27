import { useSyncExternalStore } from "react";

/**
 * usePrefersReducedMotion returns `true` when the user has asked the
 * platform to minimise non-essential motion via the
 * `prefers-reduced-motion: reduce` media query.
 *
 * Components that drive decorative or orientation-aware animation
 * should treat `true` as a signal to shorten the transition to 0 ms
 * rather than skipping state changes entirely — the state update must
 * still happen, it just should not animate.
 *
 * SSR-safe: defaults to `false` until the first client render so the
 * initial paint matches the server HTML and animations kick in only
 * after hydration lets us query the real preference.
 *
 * R-Q-24: previously used useState + useEffect to mirror the media-query
 * value into React state, which tripped react-hooks/set-state-in-effect.
 * useSyncExternalStore is the canonical pattern for subscribing React to
 * an external mutable store and avoids the cascading-render warning.
 */
export function usePrefersReducedMotion(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

function getSnapshot(): boolean {
  if (globalThis.window === undefined || typeof globalThis.window.matchMedia !== "function") {
    return false;
  }
  return globalThis.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function getServerSnapshot(): boolean {
  return false;
}

function subscribe(callback: () => void): () => void {
  if (globalThis.window === undefined || typeof globalThis.window.matchMedia !== "function") {
    return () => {};
  }
  const mql = globalThis.matchMedia("(prefers-reduced-motion: reduce)");
  // MediaQueryList.addEventListener has been the cross-browser baseline
  // since 2018, so we drop the legacy mql.addListener fallback that
  // every modern engine has now deprecated.
  mql.addEventListener("change", callback);
  return () => mql.removeEventListener("change", callback);
}
