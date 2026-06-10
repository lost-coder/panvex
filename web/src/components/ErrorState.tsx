import { useTranslation } from "react-i18next";

interface ErrorStateProps {
  /** Short error headline. Falls back to "Something went wrong". */
  title?: string;
  /** One-line human context — backend code, network hint, etc. */
  description?: string;
  onRetry?: () => void;
}

export function ErrorState({ title, description, onRetry }: Readonly<ErrorStateProps>) {
  const { t } = useTranslation("common");
  const headline = title ?? t("errorState.title");
  const detail = description;
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-16 px-6 text-center">
      <span
        aria-hidden="true"
        className="text-3xl leading-none text-status-error"
      >
        ×
      </span>
      <div className="flex flex-col items-center gap-1">
        <h3 className="text-sm font-semibold text-fg">{headline}</h3>
        {detail && <p className="text-xs text-fg-muted max-w-sm">{detail}</p>}
      </div>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="mt-1 px-3 py-1.5 text-xs border border-border-hi rounded-xs text-fg-muted hover:text-fg hover:bg-bg-hover transition-colors"
        >
          {t("errorState.retry")}
        </button>
      )}
    </div>
  );
}
