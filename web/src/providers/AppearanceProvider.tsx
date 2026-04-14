import { useQuery } from "@tanstack/react-query";
import * as React from "react";
import { useEffect, useState, type ReactNode } from "react";

import { apiClient } from "@/lib/api";
import {
  applyAppearanceAttributes,
  clearAppearanceAttributes,
  defaultAppearanceSettings,
  getAppearanceQueryKey,
  normalizeAppearanceSettings,
  resolveEffectiveAppearance
} from "@/lib/appearance";

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

export function useAppearance() {
  return React.useContext(AppearanceContext);
}

export function AppearanceProvider(props: { children: ReactNode; userID: string }) {
  const [swipeNavigation, setSwipeNavigationState] = useState(readSwipeNav);
  const [prefersDark, setPrefersDark] = useState(false);
  const appearanceQuery = useQuery({
    queryKey: getAppearanceQueryKey(props.userID),
    queryFn: () => apiClient.appearanceSettings(),
    retry: false
  });

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return;
    }
    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    const syncPreference = () => setPrefersDark(mediaQuery.matches);
    syncPreference();
    const handleChange = (event: MediaQueryListEvent) => setPrefersDark(event.matches);
    if (typeof mediaQuery.addEventListener === "function") {
      mediaQuery.addEventListener("change", handleChange);
      return () => mediaQuery.removeEventListener("change", handleChange);
    }
    mediaQuery.addListener(handleChange);
    return () => mediaQuery.removeListener(handleChange);
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

  const value: AppearanceContextValue = {
    swipeNavigation,
    setSwipeNavigation,
  };

  return (
    <AppearanceContext.Provider value={value}>
      {props.children}
    </AppearanceContext.Provider>
  );
}
