import { cn } from "@/lib/cn";

interface StatusIndicatorProps {
  status: "online" | "offline" | "connecting" | "warn";
  label?: string;
  showLabel?: boolean;
}

const statusMap = {
  online:     { dot: "bg-good", text: "text-good-text",  label: "Online" },
  offline:    { dot: "bg-bad",  text: "text-bad-text",   label: "Offline" },
  connecting: { dot: "bg-warn", text: "text-warn-text",  label: "Connecting" },
  warn:       { dot: "bg-warn", text: "text-warn-text",  label: "Warning" },
};

export function StatusIndicator({ status, label, showLabel = true }: StatusIndicatorProps) {
  const map = statusMap[status];
  const displayLabel = label ?? map.label;
  return (
    <div className="flex items-center gap-1.5 text-[12px] font-semibold font-sans">
      <span className={cn("w-2 h-2 rounded-full", map.dot)} />
      {showLabel && <span className={cn(map.text)}>{displayLabel}</span>}
    </div>
  );
}
