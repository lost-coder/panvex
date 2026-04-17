// Fallback rendered by the root ErrorBoundary when a render error escapes
// a container. It is intentionally minimal: no hooks, no API calls —
// everything must work even if the store/query-client state is corrupted.
export function AppErrorFallback({ error }: { error: Error }) {
  const reload = () => window.location.reload();
  return (
    <div
      role="alert"
      className="min-h-screen flex items-center justify-center bg-bg p-8"
    >
      <div className="max-w-md text-center space-y-4">
        <div className="text-4xl">⚠️</div>
        <h1 className="text-xl font-semibold text-fg">Something went wrong</h1>
        <p className="text-sm text-fg-muted">
          The dashboard hit an unexpected error and can&apos;t continue. Reload
          the page to recover. If the problem persists, check the browser
          console and report the error ID below.
        </p>
        <pre className="text-xs text-fg-muted bg-bg-card p-3 rounded-xs overflow-x-auto text-left">
          {error.name}: {error.message}
        </pre>
        <button
          type="button"
          onClick={reload}
          className="inline-flex items-center justify-center h-10 px-6 rounded-xs bg-accent text-white text-sm font-medium hover:bg-accent/80"
        >
          Reload
        </button>
      </div>
    </div>
  );
}
