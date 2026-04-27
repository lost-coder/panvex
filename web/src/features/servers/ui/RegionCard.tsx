import { cn } from "@/ui/lib/cn";
import type { Status } from "@/ui/tokens/colors";
import { StatusDot } from "@/ui/primitives/StatusDot";

export interface RegionCardProps {
  name: string;
  status: Status;
  nodeCount: number;
  dcCount: number;
  clients: number;
  load: number;
  onClick?: () => void;
  className?: string;
}

export function RegionCard({
  name,
  status,
  nodeCount,
  dcCount,
  clients,
  load,
  onClick,
  className,
}: Readonly<RegionCardProps>) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex flex-col gap-2 rounded-xs bg-bg-card p-3 min-w-[150px] snap-start",
        "border border-transparent hover:border-border-hi hover:bg-bg-card-hi transition-colors",
        "text-left",
        className,
      )}
    >
      <div className="flex items-center gap-2">
        <StatusDot status={status} size="md" />
        <span className="font-semibold text-sm text-fg">{name}</span>
      </div>
      <div className="grid grid-cols-2 gap-x-4 gap-y-1">
        <Stat value={nodeCount} label="Nodes" />
        <Stat value={dcCount} label="DCs" />
        <Stat value={clients} label="Clients" />
        <Stat value={`${load}%`} label="Load" />
      </div>
    </button>
  );
}

function Stat({ value, label }: Readonly<{ value: number | string; label: string }>) {
  return (
    <div className="flex items-baseline gap-1">
      <span className="text-xs font-mono font-medium text-fg">{value}</span>
      <span className="text-[10px] text-fg-muted">{label}</span>
    </div>
  );
}
