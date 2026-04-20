// src/compositions/NodeSelector.tsx
import { useState } from "react";
import { cn } from "@/ui/lib/cn";
import { StatusDot } from "@/ui/primitives/StatusDot";
import { Input } from "@/ui/base/input";
import type { NodeSelectorProps } from "@/shared/api/types-pages/pages";

export function NodeSelector({
  nodes,
  selectedNodeIds,
  onChange,
  className,
}: NodeSelectorProps & { className?: string }) {
  const [search, setSearch] = useState("");

  const filtered = nodes.filter(
    (n) =>
      n.name.toLowerCase().includes(search.toLowerCase()) ||
      n.fleetGroup.toLowerCase().includes(search.toLowerCase()),
  );

  function toggle(id: string) {
    onChange(
      selectedNodeIds.includes(id)
        ? selectedNodeIds.filter((x) => x !== id)
        : [...selectedNodeIds, id],
    );
  }

  return (
    <div className={cn("flex flex-col gap-3", className)}>
      <Input
        type="text"
        placeholder="Search nodes..."
        value={search}
        onChange={(e) => setSearch(e.target.value)}
      />
      <div
        role="group"
        aria-label={`Node selection — ${selectedNodeIds.length} of ${nodes.length} selected`}
        className="max-h-[240px] overflow-y-auto rounded-xs border border-border divide-y divide-border"
      >
        {filtered.length === 0 && (
          <div className="px-3 py-4 text-sm text-fg-muted text-center">No nodes found</div>
        )}
        {filtered.map((node) => (
          <label
            key={node.id}
            className="flex items-center gap-3 px-3 py-2 hover:bg-bg-card-hover transition-colors cursor-pointer"
          >
            <input
              type="checkbox"
              checked={selectedNodeIds.includes(node.id)}
              onChange={() => toggle(node.id)}
              className="rounded border-border"
              aria-label={`${node.name} (${node.fleetGroup})`}
            />
            <span className="text-sm text-fg flex-1">{node.name}</span>
            <StatusDot status={node.status} />
            <span className="text-[10px] text-fg-muted">{node.fleetGroup}</span>
          </label>
        ))}
      </div>
      <div className="text-xs text-fg-muted">
        {selectedNodeIds.length} of {nodes.length} nodes selected
      </div>
    </div>
  );
}
