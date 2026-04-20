import { useEffect, useState } from "react";

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
 */
export function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(false);

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return;
    }
    const mql = window.matchMedia("(prefers-reduced-motion: reduce)");
    setReduced(mql.matches);

    const handler = (event: MediaQueryListEvent) => {
      setReduced(event.matches);
    };
    if (typeof mql.addEventListener === "function") {
      mql.addEventListener("change", handler);
      return () => mql.removeEventListener("change", handler);
    }
    mql.addListener(handler);
    return () => mql.removeListener(handler);
  }, []);

  return reduced;
}
