// src/compositions/FleetGroupChips.tsx
import { ChipToggle } from "@/ui/primitives/ChipToggle";
import { cn } from "@/ui/lib/cn";
import type { FleetGroupChipsProps } from "@/shared/api/types-pages/pages";

export function FleetGroupChips({
  groups,
  selected,
  onChange,
  className,
}: FleetGroupChipsProps & { className?: string }) {
  function toggle(id: string) {
    onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id]);
  }

  const totalNodes = groups
    .filter((g) => selected.includes(g.id))
    .reduce((sum, g) => sum + (g.nodeCount ?? g.agentCount ?? 0), 0);

  return (
    <div className={cn("flex flex-col gap-3", className)}>
      <div className="flex flex-wrap gap-2">
        {groups.map((g) => (
          <ChipToggle
            key={g.id}
            label={g.name ?? g.label ?? g.id}
            sublabel={`${g.nodeCount ?? g.agentCount ?? 0} nodes`}
            selected={selected.includes(g.id)}
            onClick={() => toggle(g.id)}
          />
        ))}
      </div>
      {selected.length > 0 && (
        <div className="text-xs text-accent bg-accent/8 border border-accent/20 rounded-xs px-3 py-1.5">
          <strong>{totalNodes} nodes selected</strong> — {selected.length} group
          {selected.length > 1 ? "s" : ""}
        </div>
      )}
    </div>
  );
}
