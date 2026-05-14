import React from "react";
import { useTranslation } from "react-i18next";
import { MoreVertical } from "lucide-react";

export interface ServerActionsDropdownProps {
  onReload?: (() => void) | undefined;
  onBoostDetail?: (() => void) | undefined;
  onRename?: (() => void) | undefined;
  onChangeFleetGroup?: (() => void) | undefined;
  onDeregister?: (() => void) | undefined;
}

export function ServerActionsDropdown({
  onReload,
  onBoostDetail,
  onRename,
  onChangeFleetGroup,
  onDeregister,
}: Readonly<ServerActionsDropdownProps>) {
  const { t } = useTranslation("servers");
  const [open, setOpen] = React.useState(false);
  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="p-1.5 rounded-xs hover:bg-white/10 transition-colors text-fg-muted hover:text-fg"
        title={t("detail.actions.title")}
        aria-label={t("detail.actions.title")}
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
          <button
            type="button"
            tabIndex={-1}
            aria-hidden="true"
            className="fixed inset-0 z-40 cursor-default"
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
              {t("detail.actions.reload")}
            </button>
            {onBoostDetail && (
              <button
                onClick={() => {
                  onBoostDetail();
                  setOpen(false);
                }}
                className="px-3 py-2 text-left text-sm text-fg hover:bg-bg-card-hover transition-colors"
              >
                {t("detail.actions.refreshDiagnostics")}
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
                {t("detail.actions.rename")}
              </button>
            )}
            {onChangeFleetGroup && (
              <button
                onClick={() => {
                  onChangeFleetGroup();
                  setOpen(false);
                }}
                className="px-3 py-2 text-left text-sm text-fg hover:bg-bg-card-hover transition-colors"
              >
                {t("detail.actions.changeFleetGroup")}
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
                  {t("detail.actions.deregister")}
                </button>
              </>
            )}
          </div>
        </>
      )}
    </div>
  );
}
