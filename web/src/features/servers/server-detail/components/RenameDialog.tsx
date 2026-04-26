import { useState } from "react";

import { Sheet, SheetBody, SheetContent, SheetHeader, SheetTitle } from "@/ui";

/**
 * Sheet-hosted form for renaming a server. Trims input and only fires
 * `onRename` when the new name actually differs from the current one.
 */
export function RenameDialog({
  open,
  onOpenChange,
  currentName,
  onRename,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentName: string;
  onRename?: ((name: string) => void) | undefined;
}) {
  const [value, setValue] = useState(currentName);

  // Reset the field whenever the sheet opens so it picks up any
  // out-of-band rename and so cancel + reopen doesn't preserve the
  // previously-typed text.
  const handleOpenChange = (next: boolean) => {
    if (next) setValue(currentName);
    onOpenChange(next);
  };

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent>
        <SheetHeader>
          <SheetTitle>Rename Server</SheetTitle>
        </SheetHeader>
        <SheetBody>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              const trimmed = value.trim();
              if (trimmed && trimmed !== currentName) {
                onRename?.(trimmed);
              }
              onOpenChange(false);
            }}
            className="flex flex-col gap-4"
          >
            <label className="flex flex-col gap-1.5">
              <span className="text-sm text-fg-muted">Server Name</span>
              <input
                type="text"
                value={value}
                onChange={(e) => setValue(e.target.value)}
                className="rounded-xs border border-border bg-bg px-3 py-2 text-sm text-fg focus:outline-none focus:ring-2 focus:ring-accent"
                autoFocus
              />
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
                disabled={!value.trim() || value.trim() === currentName}
                className="px-3 py-1.5 text-sm rounded-xs bg-accent text-white hover:bg-accent/90 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Save
              </button>
            </div>
          </form>
        </SheetBody>
      </SheetContent>
    </Sheet>
  );
}
