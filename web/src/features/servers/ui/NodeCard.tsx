import { cn } from "@/ui/lib/cn";
import type { Status } from "@/ui/tokens/colors";
import { StatusDot } from "@/ui/primitives/StatusDot";
import { ArrowUpCircle } from "lucide-react";

export interface NodeCardProps {
  name: string;
  status: Status;
  health: number;
  cpu: number;
  mem: number;
  clients: number;
  region: string;
  alert?: string;
  /** When true, shows an update-available icon in the top-right corner. */
  updateAvailable?: boolean;
  onClick?: () => void;
  className?: string;
}

export function NodeCard({
  name,
  status,
  health,
  cpu,
  mem,
  clients,
  region,
  alert,
  updateAvailable,
  onClick,
  className,
}: Readonly<NodeCardProps>) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "relative flex flex-col gap-2 rounded-xs bg-bg-card p-3 text-left w-full",
        "border border-transparent hover:border-border-hi hover:bg-bg-card-hi transition-colors",
        alert && "border-l-[3px] border-l-status-error",
        className,
      )}
    >
      {updateAvailable && (
        <ArrowUpCircle
          className="absolute top-2 right-2 w-4 h-4 text-accent"
          aria-label="Update available"
        />
      )}
      <div className="flex items-center gap-2">
        <StatusDot status={status} size="md" />
        <span className="font-mono font-semibold text-sm text-fg flex-1 truncate">{name}</span>
        <span className="text-caption">{region}</span>
      </div>

      <div className="grid grid-cols-4 gap-2">
        <Metric value={`${health}%`} label="Health" />
        <Metric value={`${cpu}%`} label="CPU" />
        <Metric value={`${mem}%`} label="MEM" />
        <Metric value={String(clients)} label="Clients" />
      </div>

      {alert && (
        <p className="text-[11px] text-status-error font-mono leading-snug truncate">{alert}</p>
      )}
    </button>
  );
}

function Metric({ value, label }: Readonly<{ value: string; label: string }>) {
  return (
    <div className="flex flex-col">
      <span className="text-xs font-mono font-medium text-fg leading-none">{value}</span>
      <span className="text-[10px] text-fg-muted uppercase tracking-wider mt-0.5">{label}</span>
    </div>
  );
}
