import { useState } from "react";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { Button } from "@/ui/base/button";
import { FleetGroupChips } from "@/features/servers/ui/FleetGroupChips";
import { NodeSelector } from "@/features/servers/ui/NodeSelector";
import type { ClientAccessSheetProps } from "@/shared/api/types-pages/pages";

export function ClientAccessSheet({
  fleetGroups,
  nodes,
  selectedFleetGroupIds,
  selectedNodeIds,
  onFleetGroupsChange,
  onNodesChange,
  onSubmit,
  onCancel,
  loading,
}: ClientAccessSheetProps) {
  const [showFineTune, setShowFineTune] = useState(false);

  const groupNodeIds = nodes
    .filter((n) => selectedFleetGroupIds.includes(n.fleetGroup))
    .map((n) => n.id);

  const allSelected = [...new Set([...groupNodeIds, ...selectedNodeIds])];

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-title">Manage Access</h3>
        <p className="text-sm text-fg-muted mt-0.5">Select which nodes this client can access.</p>
      </div>

      <div>
        <SectionHeader title="Fleet Groups" />
        <FleetGroupChips
          groups={fleetGroups}
          selected={selectedFleetGroupIds}
          onChange={onFleetGroupsChange}
          className="mt-2"
        />
      </div>

      <button
        type="button"
        onClick={() => setShowFineTune(!showFineTune)}
        aria-expanded={showFineTune}
        aria-controls="client-access-finetune-section"
        className="text-xs text-fg-muted hover:text-fg text-left"
      >
        {showFineTune ? "▾" : "▸"} Fine-tune individual nodes
      </button>

      {showFineTune && (
        <div id="client-access-finetune-section">
          <NodeSelector
            nodes={nodes}
            selectedNodeIds={allSelected}
            onChange={(ids) => {
              const individualOnly = ids.filter((id) => !groupNodeIds.includes(id));
              onNodesChange(individualOnly);
            }}
          />
        </div>
      )}

      <div className="text-xs text-fg-muted">
        Total: <strong className="text-fg">{allSelected.length}</strong> nodes accessible
      </div>

      <div className="flex gap-2 justify-end mt-2">
        <Button variant="ghost" onClick={onCancel} disabled={loading}>
          Cancel
        </Button>
        <Button onClick={onSubmit} disabled={loading}>
          {loading ? "Saving..." : "Save Access"}
        </Button>
      </div>
    </div>
  );
}
