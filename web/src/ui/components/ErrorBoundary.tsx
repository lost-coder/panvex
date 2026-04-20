import { Component, type ErrorInfo, type ReactNode } from "react";

export interface ErrorBoundaryProps {
  children: ReactNode;
  fallback?: ReactNode | ((error: Error, reset: () => void) => ReactNode);
}

interface ErrorBoundaryState {
  error: Error | null;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error("[ErrorBoundary]", error, info.componentStack);
  }

  reset = (): void => {
    this.setState({ error: null });
  };

  render(): ReactNode {
    const { error } = this.state;
    const { children, fallback } = this.props;

    if (error) {
      if (typeof fallback === "function") {
        return fallback(error, this.reset);
      }
      if (fallback) {
        return fallback;
      }
      return <DefaultFallback error={error} onReset={this.reset} />;
    }

    return children;
  }
}

function DefaultFallback({ error, onReset }: { error: Error; onReset: () => void }) {
  return (
    <div className="flex min-h-[200px] flex-col items-center justify-center gap-4 rounded-lg border border-red-500/20 bg-red-500/5 p-6 text-center">
      <div className="text-sm font-medium text-red-400">Something went wrong</div>
      <pre className="max-w-md overflow-auto text-xs text-zinc-400">{error.message}</pre>
      <button
        type="button"
        onClick={onReset}
        className="rounded-md bg-zinc-800 px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:bg-zinc-700"
      >
        Try again
      </button>
    </div>
  );
}
