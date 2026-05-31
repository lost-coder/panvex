import { useId } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/ui/lib/cn";
import { usePrefersReducedMotion } from "@/ui/lib/usePrefersReducedMotion";

export interface SkeletonProps {
  className?: string | undefined;
  /**
   * When present, adds `role="status"` + `aria-label` so a screen reader
   * announces the placeholder as "loading …". Default `role` is
   * `presentation` — a bank of skeletons should announce once, not N times.
   */
  label?: string | undefined;
}

export function Skeleton({ className, label }: Readonly<SkeletonProps>) {
  const reduceMotion = usePrefersReducedMotion();
  const base = cn(
    "rounded-xs bg-bg-card-hi/70",
    !reduceMotion && "animate-pulse",
    className,
  );
  return label ? (
    <output aria-label={label} className={base} />
  ) : (
    <div aria-hidden="true" className={base} />
  );
}

export interface SkeletonRowsProps {
  count: number;
  /** Per-row height Tailwind class. Defaults to `h-12`. */
  height?: string;
  /** Aria label for the first row. Rest are silent to avoid noise.
   *  Falls back to the localised "Loading list…" when omitted. */
  label?: string;
  className?: string;
}

/**
 * Vertical stack of `count` `Skeleton` rows with consistent spacing. Use
 * while a list query is loading — mirror the row height of the real list
 * so the page doesn't jump on data arrival.
 */
export function SkeletonRows({
  count,
  height = "h-12",
  label,
  className,
}: Readonly<SkeletonRowsProps>) {
  const { t } = useTranslation("ui");
  const rowLabel = label ?? t("loadingList");
  // Stable per-instance prefix (useId) + per-row index combined makes
  // a unique non-positional key for the placeholder rows. Index alone
  // tripped Sonar S6479; a fresh UUID per render would defeat React's
  // reconciliation.
  const id = useId();
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      {Array.from({ length: count }, (_, i) => (
        <Skeleton
          key={`${id}-${i}`}
          className={cn(height, "w-full")}
          label={i === 0 ? rowLabel : undefined}
        />
      ))}
    </div>
  );
}
