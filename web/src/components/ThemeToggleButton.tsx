import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Moon, Sun } from "lucide-react";

import { apiClient } from "@/shared/api/api";
import {
  defaultAppearanceSettings,
  getAppearanceQueryKey,
  normalizeAppearanceSettings,
} from "@/shared/lib/appearance";
import type { MeResponse } from "@/shared/api/api";

/**
 * Sidebar-footer toggle that flips between dark and light themes in one
 * click. Persists via the same `/api/settings/appearance` endpoint the
 * Settings page uses — AppearanceProvider reacts to the cache refresh
 * and applies the new class to <html>.
 *
 * Resolves the current effective theme from three sources in order:
 *   1. Stored `theme` (dark / light / system) — explicit operator choice.
 *   2. System `prefers-color-scheme` when the stored value is "system".
 *   3. Dark fallback if the OS media query is unavailable.
 * Clicking always writes an explicit dark|light value so the toggle
 * behaves predictably regardless of the previous "system" state.
 */
export function ThemeToggleButton() {
  const queryClient = useQueryClient();
  const meQuery = useQuery<MeResponse>({
    queryKey: ["me"],
    queryFn: () => apiClient.me(),
  });
  const userID = meQuery.data?.id ?? "";

  const appearanceQuery = useQuery({
    queryKey: getAppearanceQueryKey(userID),
    queryFn: () => apiClient.appearanceSettings(),
    enabled: !!userID,
  });

  const mutation = useMutation({
    mutationFn: (theme: "light" | "dark") => {
      const current = normalizeAppearanceSettings(
        appearanceQuery.data ?? defaultAppearanceSettings,
      );
      return apiClient.updateAppearanceSettings({
        theme,
        density: current.density,
        help_mode: current.help_mode,
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: getAppearanceQueryKey(userID) });
    },
  });

  const stored = normalizeAppearanceSettings(
    appearanceQuery.data ?? defaultAppearanceSettings,
  ).theme;
  const systemPrefersDark =
    globalThis.window !== undefined && typeof globalThis.window.matchMedia === "function"
      ? globalThis.matchMedia("(prefers-color-scheme: dark)").matches
      : true;
  const effective: "light" | "dark" = (() => {
    if (stored === "light") return "light";
    if (stored === "dark") return "dark";
    if (systemPrefersDark) return "dark";
    return "light";
  })();
  const next: "light" | "dark" = effective === "light" ? "dark" : "light";

  return (
    <button
      type="button"
      onClick={() => mutation.mutate(next)}
      disabled={mutation.isPending || !userID}
      aria-label={`Switch to ${next} theme`}
      title={`Switch to ${next} theme`}
      className="h-11 w-11 flex items-center justify-center rounded-xs text-lg text-fg-muted hover:text-fg hover:bg-bg-hover transition-colors disabled:opacity-40 focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-1"
    >
      {effective === "light" ? (
        <Moon className="w-5 h-5" aria-hidden="true" />
      ) : (
        <Sun className="w-5 h-5" aria-hidden="true" />
      )}
    </button>
  );
}
