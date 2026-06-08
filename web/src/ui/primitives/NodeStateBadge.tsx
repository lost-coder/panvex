import { StateBadge } from "@/ui/primitives/StateBadge";
import { nodeStatePresentation, type NodeState } from "@/ui/lib/node-status";

export interface NodeStateBadgeProps {
  state: NodeState;
  /** Already-translated status word, shown on the pill for non-ok states. */
  label: string;
  className?: string | undefined;
}

/** Node-state badge — maps a NodeState to the shared StateBadge. */
export function NodeStateBadge({ state, label, className }: Readonly<NodeStateBadgeProps>) {
  const pres = nodeStatePresentation(state);
  return <StateBadge tone={pres.tone} glyph={pres.glyph} label={label} className={className} />;
}
