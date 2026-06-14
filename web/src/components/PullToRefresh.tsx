import { RefreshCw } from "lucide-react";

import { cn } from "@/ui";
import { usePullToRefresh } from "@/shared/hooks/usePullToRefresh";

// Stage 5: renders the pull-to-refresh affordance (a fixed indicator that
// tracks the drag and spins while refreshing). Touch-only; the hook no-ops
// on hover-capable devices so it's invisible on desktop. The host wires
// `onRefresh` to a query invalidation.
export function PullToRefresh({ onRefresh }: Readonly<{ onRefresh: () => Promise<unknown> | void }>) {
  const { pull, refreshing, armed } = usePullToRefresh(onRefresh);

  if (pull <= 0 && !refreshing) return null;

  return (
    <div
      aria-hidden={!refreshing}
      role="status"
      className="md:hidden fixed inset-x-0 top-0 z-40 flex justify-center pointer-events-none"
      style={{ transform: `translateY(${Math.max(pull - 24, 0)}px)`, opacity: Math.min(pull / 70 + (refreshing ? 1 : 0), 1) }}
    >
      <div className="mt-2 rounded-full bg-bg-card border border-border-hi shadow-lg p-2 text-fg-muted">
        <RefreshCw
          size={18}
          className={cn(
            "transition-transform",
            refreshing && "animate-spin",
          )}
          style={refreshing ? undefined : { transform: `rotate(${pull * 3}deg)`, color: armed ? "var(--color-accent)" : undefined }}
          aria-hidden="true"
        />
      </div>
    </div>
  );
}
