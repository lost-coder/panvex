import { useSyncExternalStore } from "react";

// Tailwind `md` breakpoint — the line the server-detail page (and any other
// dual-layout view) flips between its mobile and desktop presentations.
const QUERY = "(min-width: 768px)";

function hasMatchMedia(): boolean {
  return (
    globalThis.window !== undefined && typeof globalThis.window.matchMedia === "function"
  );
}

function getSnapshot(): boolean {
  // Default to the desktop (richer) layout when matchMedia is unavailable
  // (SSR / jsdom) so server HTML and tests get a deterministic tree.
  if (!hasMatchMedia()) return true;
  return globalThis.matchMedia(QUERY).matches;
}

function getServerSnapshot(): boolean {
  return true;
}

function subscribe(callback: () => void): () => void {
  if (!hasMatchMedia()) return () => {};
  const mql = globalThis.matchMedia(QUERY);
  mql.addEventListener("change", callback);
  return () => mql.removeEventListener("change", callback);
}

/**
 * useIsDesktop — true at/above the Tailwind `md` breakpoint (768px).
 *
 * Lets a component render exactly ONE of two breakpoint-specific layouts
 * instead of mounting both and CSS-hiding one (which doubles render cost and
 * mounts effects/lazy chunks for the hidden tree). Mirrors
 * usePrefersReducedMotion: useSyncExternalStore + a matchMedia guard, so it
 * is SSR/jsdom-safe and avoids the set-state-in-effect anti-pattern.
 */
export function useIsDesktop(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
