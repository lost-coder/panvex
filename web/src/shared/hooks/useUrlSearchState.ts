// P2-UX-05: URL query-param persistence for list-view state.
//
// TanStack Router's `useSearch` is the "idiomatic" path for typed search
// params, but it requires `validateSearch` wired into every route — a much
// bigger change than the UX-05 scope asks for. Instead, this hook reads/
// writes `window.location.search` directly and uses `history.replaceState`
// to avoid polluting the back/forward stack with every keystroke. That
// keeps the change surgical: containers opt-in one key at a time, no
// route-level plumbing required.
//
// Values are coerced to strings. Callers handle their own parsing when
// they need richer types (sort direction, view mode enums, etc.).

import { useCallback, useEffect, useState } from "react";

function readParam(key: string, fallback: string): string {
  if (typeof window === "undefined") return fallback;
  const params = new URLSearchParams(window.location.search);
  const v = params.get(key);
  return v ?? fallback;
}

function writeParam(key: string, value: string, fallback: string) {
  if (typeof window === "undefined") return;
  const params = new URLSearchParams(window.location.search);
  if (!value || value === fallback) {
    params.delete(key);
  } else {
    params.set(key, value);
  }
  const qs = params.toString();
  const next = `${window.location.pathname}${qs ? `?${qs}` : ""}${window.location.hash}`;
  // replaceState — typing into a search box should not spam history
  // entries. Users only expect one entry per route navigation.
  window.history.replaceState(window.history.state, "", next);
}

/**
 * Bind a single URL query parameter to React state.
 *
 * @param key       URL parameter name (e.g. "q", "status", "view").
 * @param fallback  Default when the param is absent. Writing the fallback
 *                  removes the param from the URL so canonical URLs stay
 *                  clean when the filter is at its default.
 */
export function useUrlSearchState(
  key: string,
  fallback: string,
): [string, (next: string) => void] {
  const [value, setValue] = useState<string>(() => readParam(key, fallback));

  // Keep state in sync with forward/back navigation. Without this, the
  // hook would only read the URL on mount and miss popstate updates.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const onPop = () => setValue(readParam(key, fallback));
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, [key, fallback]);

  const update = useCallback(
    (next: string) => {
      setValue(next);
      writeParam(key, next, fallback);
    },
    [key, fallback],
  );

  return [value, update];
}
