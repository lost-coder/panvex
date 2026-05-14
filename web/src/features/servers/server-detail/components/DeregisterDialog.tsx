import { useTranslation } from "react-i18next";

/**
 * Custom-rendered confirmation dialog (intentionally not a Sheet — it's
 * a destructive confirm that needs the standard centred-modal feel,
 * with backdrop click and a clear destructive Confirm button).
 */
export function DeregisterDialog({
  open,
  onClose,
  onConfirm,
}: Readonly<{
  open: boolean;
  onClose: () => void;
  onConfirm?: (() => void) | undefined;
}>) {
  const { t } = useTranslation("servers");
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <button
        type="button"
        aria-label={t("delete.dismiss")}
        onClick={onClose}
        className="absolute inset-0 bg-black/60 cursor-default"
      />
      <div className="relative z-10 bg-bg-card border border-border rounded-lg shadow-xl p-6 max-w-sm w-full mx-4">
        <h3 className="text-base font-semibold text-fg mb-2">{t("delete.title")}</h3>
        <p className="text-sm text-fg-muted mb-4">{t("delete.body")}</p>
        <div className="flex justify-end gap-2">
          <button
            onClick={onClose}
            className="px-3 py-1.5 text-sm rounded-xs border border-border text-fg hover:bg-bg-card-hover transition-colors"
          >
            {t("delete.cancel")}
          </button>
          <button
            onClick={() => {
              onConfirm?.();
              onClose();
            }}
            className="px-3 py-1.5 text-sm rounded-xs bg-status-error text-white hover:bg-status-error/90 transition-colors"
          >
            {t("delete.confirm")}
          </button>
        </div>
      </div>
    </div>
  );
}
