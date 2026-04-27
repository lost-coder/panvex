import { cn } from "@/ui/lib/cn";

export interface ClientsRowProps {
  total: number;
  active: number;
  className?: string;
}

export function ClientsRow({ total, active, className }: Readonly<ClientsRowProps>) {
  return (
    <div className={cn("flex gap-4", className)}>
      <div className="flex items-baseline gap-2">
        <span className="text-2xl font-mono font-bold text-fg leading-none">
          {total.toLocaleString()}
        </span>
        <span className="text-caption uppercase tracking-wider">Total clients</span>
      </div>
      <div className="flex items-baseline gap-2">
        <span className="text-2xl font-mono font-bold text-status-ok leading-none">
          {active.toLocaleString()}
        </span>
        <span className="text-caption uppercase tracking-wider">Active</span>
      </div>
    </div>
  );
}
