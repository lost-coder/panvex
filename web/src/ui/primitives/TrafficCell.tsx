import { cn } from "@/ui/lib/cn";

export interface TrafficCellProps {
  bytes: number;
  label?: string;
  className?: string;
}

function formatTraffic(bytes: number): { value: string; unit: string } {
  if (bytes < 1024) return { value: String(bytes), unit: "B" };
  if (bytes < 1024 ** 2) return { value: (bytes / 1024).toFixed(1), unit: "KB" };
  if (bytes < 1024 ** 3) return { value: (bytes / 1024 ** 2).toFixed(1), unit: "MB" };
  if (bytes < 1024 ** 4) return { value: (bytes / 1024 ** 3).toFixed(2), unit: "GB" };
  return { value: (bytes / 1024 ** 4).toFixed(2), unit: "TB" };
}

export function TrafficCell({ bytes, label, className }: Readonly<TrafficCellProps>) {
  const { value, unit } = formatTraffic(bytes);
  return (
    <span className={cn("inline-flex items-baseline gap-0.5 font-mono", className)}>
      <span className="text-sm font-medium text-fg">{value}</span>
      <span className="text-nano text-fg-muted">{unit}</span>
      {label && <span className="text-nano text-fg-muted ml-1">{label}</span>}
    </span>
  );
}
