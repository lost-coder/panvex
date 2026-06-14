import { useQuery } from "@tanstack/react-query";
import * as React from "react";
import { useEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from "react";

import { settingsApi } from "@/shared/api/settings";
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

// usePrefersDarkScheme subscribes to the OS-level "prefers-color-scheme"
// media query without setState-in-effect (react-hooks rule). The store
// pattern lets React derive the snapshot synchronously and only re-render
// when the OS actually flips the preference.
function usePrefersDarkScheme(): boolean {
  return useSyncExternalStore(
    (notify) => {
      if (globalThis.window === undefined || typeof globalThis.matchMedia !== "function") {
        return () => {};
      }
      const mediaQuery = globalThis.matchMedia("(prefers-color-scheme: dark)");
      mediaQuery.addEventListener("change", notify);
      return () => mediaQuery.removeEventListener("change", notify);
    },
    () => {
      if (globalThis.window === undefined || typeof globalThis.matchMedia !== "function") {
        return false;
      }
      return globalThis.matchMedia("(prefers-color-scheme: dark)").matches;
    },
    () => false,
  );
}

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
  const prefersDark = usePrefersDarkScheme();
  const appearanceQuery = useQuery({
    queryKey: getAppearanceQueryKey(props.userID),
    queryFn: () => settingsApi.appearanceSettings(),
    retry: false
  });

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
