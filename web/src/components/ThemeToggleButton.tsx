import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Moon, Sun } from "lucide-react";

import { cn } from "@/ui/lib/cn";
import { apiClient } from "@/shared/api/api";
import { authKeys } from "@/features/auth/queryKeys";
import {
  defaultAppearanceSettings,
  getAppearanceQueryKey,
  normalizeAppearanceSettings,
} from "@/shared/lib/appearance";
import type { MeResponse } from "@/shared/api/api";

export interface ThemeToggleButtonProps {
  /**
   * Renders the button in full-width with a label, matching the Log out
   * row in expanded sidebars. Defaults to compact icon-only.
   */
  expanded?: boolean;
}

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
export function ThemeToggleButton({ expanded = false }: Readonly<ThemeToggleButtonProps> = {}) {
  const queryClient = useQueryClient();
  const meQuery = useQuery<MeResponse>({
    queryKey: authKeys.me(),
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

  const label = `Switch to ${next} theme`;
  return (
    <button
      type="button"
      onClick={() => mutation.mutate(next)}
      disabled={mutation.isPending || !userID}
      aria-label={label}
      title={label}
      className={cn(
        "flex items-center rounded-xs transition-colors",
        "text-fg-muted hover:text-fg hover:bg-bg-hover",
        "focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-1",
        "disabled:opacity-40",
        expanded
          ? "w-full gap-3 h-11 px-3 text-sm"
          : "justify-center h-11 w-11 text-lg",
      )}
    >
      {effective === "light" ? (
        <Moon className="w-5 h-5 shrink-0" aria-hidden="true" />
      ) : (
        <Sun className="w-5 h-5 shrink-0" aria-hidden="true" />
      )}
      {expanded && <span>{next === "dark" ? "Dark theme" : "Light theme"}</span>}
    </button>
  );
}
