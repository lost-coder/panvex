import { useQuery } from "@tanstack/react-query";
import * as React from "react";
import { useEffect, useMemo, useState, type ReactNode } from "react";

import { apiClient } from "@/shared/api/api";
import {
  applyAppearanceAttributes,
  clearAppearanceAttributes,
  defaultAppearanceSettings,
  getAppearanceQueryKey,
  normalizeAppearanceSettings,
  resolveEffectiveAppearance
} from "@/shared/lib/appearance";

const SWIPE_NAV_KEY = "panvex_swipe_nav";

function readSwipeNav(): boolean {
  try {
    const stored = localStorage.getItem(SWIPE_NAV_KEY);
    if (stored === "false") return false;
    return true; // default to true
  } catch {
    return true;
  }
}

interface AppearanceContextValue {
  swipeNavigation: boolean;
  setSwipeNavigation: (value: boolean) => void;
}

const AppearanceContext = React.createContext<AppearanceContextValue>({
  swipeNavigation: true,
  setSwipeNavigation: () => {},
});

// R-Q-24: provider files intentionally co-locate the context, the hook,
// and the Provider component so consumers have a single import path.
// React-refresh requires component-only exports; this rule is informational
// for HMR and does not affect production behaviour.
// eslint-disable-next-line react-refresh/only-export-components
export function useAppearance() {
  return React.useContext(AppearanceContext);
}

export function AppearanceProvider(props: Readonly<{ children: ReactNode; userID: string }>) {
  const [swipeNavigation, setSwipeNavigationState] = useState(readSwipeNav);
  const [prefersDark, setPrefersDark] = useState(false);
  const appearanceQuery = useQuery({
    queryKey: getAppearanceQueryKey(props.userID),
    queryFn: () => apiClient.appearanceSettings(),
    retry: false
  });

  useEffect(() => {
    if (globalThis.window === undefined || typeof globalThis.window.matchMedia !== "function") {
      return;
    }
    const mediaQuery = globalThis.matchMedia("(prefers-color-scheme: dark)");
    setPrefersDark(mediaQuery.matches);
    const handleChange = (event: MediaQueryListEvent) => setPrefersDark(event.matches);
    mediaQuery.addEventListener("change", handleChange);
    return () => mediaQuery.removeEventListener("change", handleChange);
  }, []);

  useEffect(() => {
    const root = document.documentElement;
    const appearance = normalizeAppearanceSettings(appearanceQuery.data ?? defaultAppearanceSettings);
    const effectiveAppearance = resolveEffectiveAppearance(appearance, prefersDark);
    applyAppearanceAttributes(root, effectiveAppearance);
  }, [appearanceQuery.data, prefersDark]);

  useEffect(() => {
    const root = document.documentElement;
    return () => { clearAppearanceAttributes(root); };
  }, []);

  const setSwipeNavigation = React.useCallback((value: boolean) => {
    setSwipeNavigationState(value);
    try {
      localStorage.setItem(SWIPE_NAV_KEY, String(value));
    } catch {
      // Storage full or unavailable; ignore.
    }
  }, []);

  const value = useMemo<AppearanceContextValue>(
    () => ({ swipeNavigation, setSwipeNavigation }),
    [swipeNavigation, setSwipeNavigation],
  );

  return (
    <AppearanceContext.Provider value={value}>
      {props.children}
    </AppearanceContext.Provider>
  );
}
