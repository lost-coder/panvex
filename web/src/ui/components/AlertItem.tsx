import { cn } from "@/ui/lib/cn";

export type AlertSeverity = "crit" | "warn" | "info";

export interface AlertItemProps {
  severity: AlertSeverity;
  message: string;
  source?: string;
  timestamp?: string;
  className?: string;
}

const borderColor = {
  crit: "border-l-status-error",
  warn: "border-l-status-warn",
  info: "border-l-accent",
} as const;

const severityBadge = {
  crit: "bg-status-error/15 text-status-error",
  warn: "bg-status-warn/15 text-status-warn",
  info: "bg-accent/15 text-accent",
} as const;

export function AlertItem({ severity, message, source, timestamp, className }: AlertItemProps) {
  return (
    <div
      className={cn(
        "rounded-xs bg-bg-card border-l-[3px] px-3 py-2.5",
        borderColor[severity],
        severity === "crit" && "[--alert-color:theme(colors.status.error)] animate-alert-pulse",
        className,
      )}
    >
      <div className="flex items-start gap-2">
        <span
          className={cn(
            "shrink-0 rounded px-1.5 py-0.5 text-[10px] font-mono font-semibold uppercase leading-none",
            severityBadge[severity],
          )}
        >
          {severity}
        </span>
        <p className="text-sm text-fg leading-snug flex-1">{message}</p>
      </div>
      {(source || timestamp) && (
        <div className="flex items-center gap-2 mt-1.5 ml-[38px]">
          {source && <span className="text-[11px] font-mono text-fg-muted">{source}</span>}
          {timestamp && <span className="text-caption/60">{timestamp}</span>}
        </div>
      )}
    </div>
  );
}
