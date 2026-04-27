import { lazy, Suspense } from "react";
import { cn } from "@/ui/lib/cn";
import type { SwipeTabViewProps } from "./internal/SwipeTabViewInner";

export type { SwipeTab, SwipeTabViewProps } from "./internal/SwipeTabViewInner";

// framer-motion + AnimatePresence pull ~40 kB (gzipped) of animation
// runtime. Most pages that compose SwipeTabView are interactive but the
// motion code is only needed once the user actually swipes a tab. Wrap
// the implementation in React.lazy so the chunk streams on first paint
// of the tab view rather than the route's initial JS budget.
const SwipeTabViewInner = lazy(() => import("./internal/SwipeTabViewInner"));

// Fallback mirrors the static layout the tabs settle into so the page
// does not flash while motion streams: a plain header strip plus a
// content well that takes the same min-height.
function SwipeTabViewFallback({ tabs, activeTab, className }: Readonly<SwipeTabViewProps>) {
  const active = tabs.find((t) => t.id === activeTab) ?? tabs[0];
  return (
    <div className={cn("flex flex-col", className)}>
      <div
        role="tablist"
        aria-label="Loading tabs"
        className="flex items-center gap-2 border-b border-border px-4"
      >
        {tabs.map((t) => (
          <span
            key={t.id}
            className={cn(
              "py-2 text-sm",
              t.id === active?.id ? "text-fg" : "text-fg-muted",
            )}
          >
            {t.label}
          </span>
        ))}
      </div>
      <div className="min-h-[200px]">{active?.content}</div>
    </div>
  );
}

export function SwipeTabView(props: Readonly<SwipeTabViewProps>) {
  return (
    <Suspense fallback={<SwipeTabViewFallback {...props} />}>
      <SwipeTabViewInner {...props} />
    </Suspense>
  );
}
