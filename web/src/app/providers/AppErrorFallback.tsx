// Fallback rendered by the root ErrorBoundary when a render error escapes
// a container. It is intentionally minimal: no hooks, no API calls —
// everything must work even if the store/query-client state is corrupted.
import i18next from "i18next";

export function AppErrorFallback({ error }: Readonly<{ error: Error }>) {
  // No hooks by design (state may be corrupted when this renders);
  // i18next's global instance is initialized in main.tsx before render.
  const t = i18next.getFixedT(null, "ui");
  const reload = () => globalThis.location.reload();
  return (
    <div
      role="alert"
      className="min-h-screen flex items-center justify-center bg-bg p-8"
    >
      <div className="max-w-md text-center space-y-4">
        <div className="text-4xl">⚠️</div>
        <h1 className="text-xl font-semibold text-fg">{t("errorFallback.title")}</h1>
        <p className="text-sm text-fg-muted">
          {t("errorFallback.body")}
        </p>
        <pre className="text-xs text-fg-muted bg-bg-card p-3 rounded-xs overflow-x-auto text-left">
          {error.name}: {error.message}
        </pre>
        <button
          type="button"
          onClick={reload}
          className="inline-flex items-center justify-center h-10 px-6 rounded-xs bg-accent text-white text-sm font-medium hover:bg-accent/80"
        >
          {t("errorFallback.reload")}
        </button>
      </div>
    </div>
  );
}
