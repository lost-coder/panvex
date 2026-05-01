import { cn } from "@/ui/lib/cn";

export interface FallbackBannerProps {
  durationSeconds: number;
  escalated: boolean;
}

export function FallbackBanner({
  durationSeconds,
  escalated,
}: Readonly<FallbackBannerProps>) {
  const severity = escalated ? "critical" : "warn";
  const headline = escalated
    ? "ME pool down — fallback active"
    : "Running on direct fallback";
  const body = escalated
    ? "ME pool has been unavailable for over 30 minutes. Investigate ME diagnostics."
    : "ME pool currently unavailable; traffic flowing via direct fallback.";

  return (
    <div
      data-severity={severity}
      className={cn(
        "rounded-md border p-3 text-sm",
        escalated
          ? "bg-status-error-soft border-status-error text-status-error"
          : "bg-status-warning-soft border-status-warning text-status-warning",
      )}
    >
      <strong>{headline}</strong>
      <p className="mt-1 text-fg">
        {body} Active for {Math.round(durationSeconds / 60)} min.
      </p>
    </div>
  );
}
