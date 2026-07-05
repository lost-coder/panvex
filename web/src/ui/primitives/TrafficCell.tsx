import { cn } from "@/ui/lib/cn";
import { formatBytesParts } from "@/ui/lib/format";

export interface TrafficCellProps {
  bytes: number;
  label?: string;
  className?: string;
}

export function TrafficCell({ bytes, label, className }: Readonly<TrafficCellProps>) {
  const { value, unit } = formatBytesParts(bytes);
  return (
    <span className={cn("inline-flex items-baseline gap-0.5 font-mono", className)}>
      <span className="text-sm font-medium text-fg">{value}</span>
      <span className="text-nano text-fg-muted">{unit}</span>
      {label && <span className="text-nano text-fg-muted ml-1">{label}</span>}
    </span>
  );
}
