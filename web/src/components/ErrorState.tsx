export function ErrorState({ message, onRetry }: { message?: string; onRetry?: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center h-64 gap-3 text-fg-muted">
      <p className="text-sm">{message ?? "Something went wrong"}</p>
      {onRetry && (
        <button
          onClick={onRetry}
          className="px-3 py-1.5 text-sm border border-border rounded-xs hover:bg-bg-card-hover transition-colors"
        >
          Retry
        </button>
      )}
    </div>
  );
}
