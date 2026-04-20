// 2.5: lightweight loading skeletons. Kept local here so the first
// migration PR does not have to touch the UI-kit — this component will
// move to src/ui/ in Phase 4. Lines wrap the raw Tailwind so call-sites
// do not need to remember the shimmer classes.

import { usePrefersReducedMotion } from "@/ui";

interface SkeletonProps {
  className?: string;
  /**
   * Optional `role="status"` + label so screen readers announce the
   * placeholder as "loading …". Default `role` is `presentation`
   * because a bank of skeletons announced repeatedly is noisy.
   */
  label?: string;
}

export function Skeleton({ className = "", label }: SkeletonProps) {
  const reduceMotion = usePrefersReducedMotion();
  const base =
    "rounded bg-bg-card-hi/70 " +
    (reduceMotion ? "" : "animate-pulse ");
  return label ? (
    <div role="status" aria-label={label} className={`${base}${className}`} />
  ) : (
    <div role="presentation" className={`${base}${className}`} />
  );
}

/**
 * SkeletonRows renders N Skeleton blocks in a column with consistent
 * spacing. The first row carries the accessible label so listeners
 * hear one "loading list" announcement, not N.
 */
export function SkeletonRows({
  count,
  height = "h-12",
  label = "Загрузка списка…",
}: {
  count: number;
  height?: string;
  label?: string;
}) {
  return (
    <div className="flex flex-col gap-2">
      {Array.from({ length: count }).map((_, i) => (
        <Skeleton
          key={i}
          className={`${height} w-full`}
          label={i === 0 ? label : undefined}
        />
      ))}
    </div>
  );
}
