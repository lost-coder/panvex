import { useEffect, useRef, useState } from "react";

import { Sheet, SheetBody, SheetContent, SheetHeader, SheetTitle } from "@/ui";
import type { FleetGroupEntry } from "@/shared/api/api";

/**
 * Sheet-hosted form for moving a server between fleet groups. Only
 * fires `onChange` when the selected group differs from the current
 * one — otherwise the close button is the same as Cancel.
 */
export function ChangeFleetGroupDialog({
  open,
  onOpenChange,
  currentFleetGroupId,
  fleetGroups,
  onChange,
}: Readonly<{
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentFleetGroupId: string;
  fleetGroups: FleetGroupEntry[];
  onChange?: ((fleetGroupId: string) => void) | undefined;
}>) {
  const [value, setValue] = useState(currentFleetGroupId);
  const selectRef = useRef<HTMLSelectElement>(null);

  // Initial focus on the select when the sheet opens. Replaces the
  // bare autoFocus prop (jsx-a11y/no-autofocus) with a controlled
  // post-mount focus call so the warning goes away without changing
  // the UX (operator opens the sheet, immediately picks a group).
  useEffect(() => {
    if (open) selectRef.current?.focus();
  }, [open]);

  // Mirrors RenameDialog: refresh the field whenever the sheet opens
  // so cancel + reopen always lands on the server's current group,
  // even if a server-side reassignment landed in the background.
  const handleOpenChange = (next: boolean) => {
    if (next) setValue(currentFleetGroupId);
    onOpenChange(next);
  };

  const dirty = value !== "" && value !== currentFleetGroupId;

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent>
        <SheetHeader>
          <SheetTitle>Change Fleet Group</SheetTitle>
        </SheetHeader>
        <SheetBody>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              if (dirty) onChange?.(value);
              onOpenChange(false);
            }}
            className="flex flex-col gap-4"
          >
            <label className="flex flex-col gap-1.5">
              <span className="text-sm text-fg-muted">Fleet Group</span>
              <select
                ref={selectRef}
                value={value}
                onChange={(e) => setValue(e.target.value)}
                className="rounded-xs border border-border bg-bg px-3 py-2 text-sm text-fg focus:outline-none focus:ring-2 focus:ring-accent"
              >
                {fleetGroups.length === 0 ? (
                  <option value="">No groups available</option>
                ) : null}
                {fleetGroups.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.label || g.name}
                    {g.id === currentFleetGroupId ? " (current)" : ""}
                  </option>
                ))}
              </select>
              <span className="text-[11px] font-mono text-fg-muted">
                Reassigning the server moves it to the target group's
                fleet-wide client deployments. The agent stays online —
                no re-enrollment needed.
              </span>
            </label>
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={() => onOpenChange(false)}
                className="px-3 py-1.5 text-sm rounded-xs border border-border text-fg hover:bg-bg-card-hover transition-colors"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={!dirty}
                className="px-3 py-1.5 text-sm rounded-xs bg-accent text-white hover:bg-accent/90 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Move
              </button>
            </div>
          </form>
        </SheetBody>
      </SheetContent>
    </Sheet>
  );
}
