import * as React from "react";
import { motion, AnimatePresence, type PanInfo } from "framer-motion";
import { cn } from "@/ui/lib/cn";
import { usePrefersReducedMotion } from "@/ui/lib/usePrefersReducedMotion";

export interface SwipeTab {
  id: string;
  label: string;
  content: React.ReactNode;
}

export interface SwipeTabViewProps {
  tabs: SwipeTab[];
  activeTab?: string;
  onTabChange?: (tabId: string) => void;
  swipeEnabled?: boolean;
  className?: string;
}

const SWIPE_THRESHOLD = 50;
const SWIPE_VELOCITY = 500;

// Inner motion-using implementation. Exported as default so the
// public SwipeTabView wrapper (parent file) can React.lazy() it and
// stream framer-motion only when the tab view actually renders.
function SwipeTabViewInner({
  tabs,
  activeTab,
  onTabChange,
  swipeEnabled = true,
  className,
}: Readonly<SwipeTabViewProps>) {
  const [activeIndex, setActiveIndex] = React.useState(() => {
    const idx = tabs.findIndex((t) => t.id === activeTab);
    return Math.max(idx, 0);
  });
  const reduceMotion = usePrefersReducedMotion();

  React.useEffect(() => {
    if (activeTab) {
      const idx = tabs.findIndex((t) => t.id === activeTab);
      // R-Q-24: this effect mirrors the controlled `activeTab` prop into
      // the internal swiper index — by-design synchronisation, not a
      // cascading render that needs unwinding.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      if (idx >= 0) setActiveIndex(idx);
    }
  }, [activeTab, tabs]);

  function go(direction: number) {
    const next = activeIndex + direction;
    const target = tabs[next];
    if (!target) return;
    setActiveIndex(next);
    onTabChange?.(target.id);
  }

  function handleDragEnd(_: unknown, info: PanInfo) {
    if (!swipeEnabled) return;
    const { offset, velocity } = info;
    if (Math.abs(offset.x) > SWIPE_THRESHOLD || Math.abs(velocity.x) > SWIPE_VELOCITY) {
      go(offset.x < 0 ? 1 : -1);
    }
  }

  return (
    <div className={cn("flex flex-col overflow-hidden", className)}>
      {/* Tab bar */}
      <div className="flex border-b border-border" role="tablist" aria-label="Content tabs">
        {tabs.map((tab, i) => (
          <button
            key={tab.id}
            role="tab"
            aria-selected={i === activeIndex}
            tabIndex={i === activeIndex ? 0 : -1}
            id={`tab-${tab.id}`}
            aria-controls={`tabpanel-${tab.id}`}
            className={cn(
              "flex-1 px-3 py-2.5 text-xs font-medium tracking-wide uppercase transition-colors",
              i === activeIndex
                ? "text-accent border-b-2 border-accent"
                : "text-fg-muted hover:text-fg",
            )}
            onClick={() => {
              setActiveIndex(i);
              onTabChange?.(tab.id);
            }}
            onKeyDown={(e) => {
              let next = -1;
              if (e.key === "ArrowRight") next = i < tabs.length - 1 ? i + 1 : 0;
              else if (e.key === "ArrowLeft") next = i > 0 ? i - 1 : tabs.length - 1;
              else if (e.key === "Home") next = 0;
              else if (e.key === "End") next = tabs.length - 1;
              const nextTab = next >= 0 ? tabs[next] : undefined;
              if (nextTab) {
                e.preventDefault();
                setActiveIndex(next);
                onTabChange?.(nextTab.id);
                document.getElementById(`tab-${nextTab.id}`)?.focus();
              }
            }}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Swipeable content */}
      <div className="relative flex-1 overflow-hidden">
        <AnimatePresence mode="popLayout" initial={false}>
          {(() => {
            const active = tabs[activeIndex];
            if (!active) return null;
            return (
              <motion.div
                key={active.id}
                // U2: honour prefers-reduced-motion by collapsing the
                // translate-in/out to an opacity-only fade with a zero
                // transition so users who opted out of motion still see
                // the tab swap, just without the slide.
                initial={reduceMotion ? { opacity: 0 } : { opacity: 0, x: 50 }}
                animate={reduceMotion ? { opacity: 1 } : { opacity: 1, x: 0 }}
                exit={reduceMotion ? { opacity: 0 } : { opacity: 0, x: -50 }}
                transition={
                  reduceMotion
                    ? { duration: 0 }
                    : { type: "spring", stiffness: 300, damping: 30 }
                }
                drag={swipeEnabled && !reduceMotion ? "x" : false}
                dragConstraints={{ left: 0, right: 0 }}
                dragElastic={0.2}
                onDragEnd={handleDragEnd}
                className="w-full"
                role="tabpanel"
                id={`tabpanel-${active.id}`}
                aria-labelledby={`tab-${active.id}`}
              >
                {active.content}
              </motion.div>
            );
          })()}
        </AnimatePresence>
      </div>
    </div>
  );
}

export default SwipeTabViewInner;
