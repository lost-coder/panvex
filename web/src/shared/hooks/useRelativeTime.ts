import { useTranslation } from "react-i18next";
import { useNowSec } from "./useNowSec";

export type RelativeTimeParts =
  | { key: "justNow" }
  | { key: "minutesAgo" | "hoursAgo" | "daysAgo"; count: number }
  | { key: "absolute" };

/**
 * Pure relative-time bucketer. Returns the i18n key (and count) for an age,
 * or `absolute` once the gap exceeds the 30-day cutoff so callers render a
 * real date instead of "412d ago". Future timestamps clamp to justNow.
 *
 * This is the single source of relative-time thresholds; the localized
 * strings and the ticking clock live in useRelativeTime.
 */
export function relativeTimeParts(nowSec: number, targetSec: number): RelativeTimeParts {
  const diff = Math.floor(nowSec - targetSec);
  if (diff < 60) return { key: "justNow" };
  if (diff < 3_600) return { key: "minutesAgo", count: Math.floor(diff / 60) };
  if (diff < 86_400) return { key: "hoursAgo", count: Math.floor(diff / 3_600) };
  if (diff < 30 * 86_400) return { key: "daysAgo", count: Math.floor(diff / 86_400) };
  return { key: "absolute" };
}

/**
 * Shared, localized, ticking relative-time formatter. Returns a function that
 * maps a unix-seconds number or an ISO/RFC3339 string to a label like
 * "5m ago" (localized via the `common` namespace), re-rendering every second
 * via useNowSec. Nullish/unparseable input collapses to "—".
 *
 * Use this instead of re-rolling a per-feature relative-time hook. For the
 * non-reactive pure case (e.g. a table cell formatter outside a component)
 * use `formatAge` from `@/ui/lib/format`.
 */
export function useRelativeTime(): (input: string | number | null | undefined) => string {
  const { t } = useTranslation("common");
  const nowSec = useNowSec();
  return (input) => {
    if (input === null || input === undefined || input === "") return "—";
    const targetSec =
      typeof input === "number" ? input : Math.floor(Date.parse(input) / 1000);
    if (!Number.isFinite(targetSec)) return "—";
    const parts = relativeTimeParts(nowSec, targetSec);
    if (parts.key === "absolute") {
      return new Date(targetSec * 1000).toLocaleDateString(undefined, {
        year: "numeric",
        month: "short",
        day: "numeric",
      });
    }
    if (parts.key === "justNow") return t("relative.justNow");
    return t(`relative.${parts.key}`, { count: parts.count });
  };
}
