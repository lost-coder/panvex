import { cn } from "@/ui/lib/cn";

export interface ConnectionBadgeProps {
  online: boolean;
  count?: number;
  className?: string;
}

export function ConnectionBadge({ online, count, className }: Readonly<ConnectionBadgeProps>) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-mono font-medium leading-none",
        online ? "bg-status-ok/10 text-status-ok" : "bg-fg-faint text-fg-muted",
        className,
      )}
    >
      <span
        className={cn(
          "h-1.5 w-1.5 rounded-full shrink-0",
          online ? "bg-status-ok animate-breathe" : "bg-fg-muted",
        )}
      />
      {online ? "online" : "offline"}
      {count !== undefined && <span className="opacity-70">({count})</span>}
    </span>
  );
}
