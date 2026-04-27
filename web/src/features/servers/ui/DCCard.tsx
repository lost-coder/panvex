import { cn } from "@/ui/lib/cn";
import type { Status } from "@/ui/tokens/colors";
import { StatusDot } from "@/ui/primitives/StatusDot";

export interface DCCardProps {
  code: string;
  city: string;
  latency: number;
  load: number;
  status: Status;
  onClick?: () => void;
  className?: string;
}

export function DCCard({ code, city, latency, load, status, onClick, className }: Readonly<DCCardProps>) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex flex-col gap-2 rounded-xs bg-bg-card p-3 min-w-[120px] snap-start",
        "border border-transparent hover:border-border-hi hover:bg-bg-card-hi transition-colors",
        "text-left",
        className,
      )}
    >
      <div className="flex items-center gap-2">
        <StatusDot status={status} size="md" animated={status === "error"} />
        <span className="font-mono font-semibold text-sm text-fg">{code}</span>
      </div>
      <span className="text-caption leading-none">{city}</span>
      <div className="flex items-center gap-3 mt-auto">
        <span className="text-xs font-mono text-fg-muted">
          {latency}
          <span className="text-fg-muted/50">ms</span>
        </span>
        <span className="text-xs font-mono text-fg-muted">
          {load}
          <span className="text-fg-muted/50">%</span>
        </span>
      </div>
    </button>
  );
}
