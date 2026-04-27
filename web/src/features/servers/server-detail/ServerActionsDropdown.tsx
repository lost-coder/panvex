import React from "react";
import { MoreVertical } from "lucide-react";

export interface ServerActionsDropdownProps {
  onReload?: (() => void) | undefined;
  onBoostDetail?: (() => void) | undefined;
  onRename?: (() => void) | undefined;
  onDeregister?: (() => void) | undefined;
}

export function ServerActionsDropdown({
  onReload,
  onBoostDetail,
  onRename,
  onDeregister,
}: Readonly<ServerActionsDropdownProps>) {
  const [open, setOpen] = React.useState(false);
  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="p-1.5 rounded-xs hover:bg-white/10 transition-colors text-fg-muted hover:text-fg"
        title="Server actions"
        aria-label="Server actions"
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <MoreVertical className="w-4 h-4" />
      </button>
      {open && (
        <>
          {/*
            The backdrop exists purely to catch outside clicks and close the
            menu. It must not be announced to assistive tech and must not take
            focus — role=presentation + aria-hidden remove it from the AX tree
            while keeping the click handler active. Keyboard users close the
            menu with Escape (handled by the menu itself / Radix in future).
          */}
          <div
            className="fixed inset-0 z-40"
            role="presentation"
            aria-hidden="true"
            onClick={() => setOpen(false)}
          />
          <div className="absolute right-0 top-full mt-1 z-50 min-w-[180px] rounded-xs bg-bg-card border border-border shadow-lg py-1 flex flex-col">
            <button
              onClick={() => {
                onReload?.();
                setOpen(false);
              }}
              className="px-3 py-2 text-left text-sm text-fg hover:bg-bg-card-hover transition-colors"
            >
              Reload Runtime
            </button>
            {onBoostDetail && (
              <button
                onClick={() => {
                  onBoostDetail();
                  setOpen(false);
                }}
                className="px-3 py-2 text-left text-sm text-fg hover:bg-bg-card-hover transition-colors"
              >
                Refresh Diagnostics
              </button>
            )}
            {onRename && (
              <button
                onClick={() => {
                  onRename();
                  setOpen(false);
                }}
                className="px-3 py-2 text-left text-sm text-fg hover:bg-bg-card-hover transition-colors"
              >
                Rename Server
              </button>
            )}
            {onDeregister && (
              <>
                <div className="h-px bg-border my-1" />
                <button
                  onClick={() => {
                    onDeregister();
                    setOpen(false);
                  }}
                  className="px-3 py-2 text-left text-sm text-status-error hover:bg-bg-card-hover transition-colors"
                >
                  Deregister Server
                </button>
              </>
            )}
          </div>
        </>
      )}
    </div>
  );
}
