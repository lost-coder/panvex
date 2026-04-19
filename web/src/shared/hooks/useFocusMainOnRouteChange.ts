import { useEffect } from "react";

/**
 * Programmatically focuses the `<main id="main-content">` landmark
 * whenever the pathname changes. Without this, keyboard and
 * screen-reader users stay focused on the sidebar link they just
 * activated — they have to Tab through the nav again to reach the new
 * page content. Focusing the landmark after navigation both announces
 * the new page (via aria-label / heading sequence) and puts the next
 * Tab keystroke inside the content area.
 *
 * `preventScroll` keeps the scroll position intact; the router already
 * resets it on navigation. We skip the initial render so first-load
 * focus remains on the browser-default target (usually <body>).
 */
export function useFocusMainOnRouteChange(pathname: string) {
  useEffect(() => {
    if (typeof document === "undefined") return;
    const main = document.getElementById("main-content");
    if (!main) return;
    main.focus({ preventScroll: true });
  }, [pathname]);
}
