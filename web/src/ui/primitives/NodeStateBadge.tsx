import { cn } from "@/ui/lib/cn";
import { StatusPill } from "@/ui/primitives/StatusPill";
import { nodeStatePresentation, type NodeState } from "@/ui/lib/node-status";

export interface NodeStateBadgeProps {
  state: NodeState;
  /** Already-translated status word, shown on the pill for non-ok states. */
  label: string;
  className?: string | undefined;
}

/**
 * Renders a loud status pill for problem states (down/offline/degraded/pending)
 * and a quiet ✓ chip for ok — the shared "node state badge" used across the
 * fleet list, server list, and server cards.
 */
export function NodeStateBadge({ state, label, className }: Readonly<NodeStateBadgeProps>) {
  const pres = nodeStatePresentation(state);
  if (state === "ok") {
    return (
      <span
        aria-hidden="true"
        className={cn(
          "inline-flex h-5 w-5 items-center justify-center rounded-full bg-status-ok/15 text-status-ok text-micro font-bold shrink-0",
          className,
        )}
      >
        {pres.glyph}
      </span>
    );
  }
  return <StatusPill tone={pres.tone} glyph={pres.glyph} label={label} className={className} />;
}
