import { ChevronDown, ChevronUp } from "lucide-react";
import { cn } from "@/lib/cn";

interface StatCardProps {
  label: string;
  value: string | number;
  change?: { direction: "up" | "down"; text: string };
  valueColor?: string;
}

export function StatCard({ label, value, change, valueColor }: StatCardProps) {
  return (
    <div className="bg-card border border-border rounded backdrop-blur-[var(--blur)] p-4">
      <div className="text-[10px] font-bold uppercase tracking-[0.1em] text-text-3">{label}</div>
      <div className={cn("text-2xl font-extrabold tracking-tight mt-1 font-sans", valueColor ?? "text-text-1")}>
        {value}
      </div>
      {change && (
        <div
          className={cn(
            "text-[11px] font-semibold mt-1 flex items-center gap-1",
            change.direction === "up" ? "text-good-text" : "text-bad-text"
          )}
        >
          {change.direction === "up" ? (
            <ChevronUp className="w-[10px] h-[10px]" />
          ) : (
            <ChevronDown className="w-[10px] h-[10px]" />
          )}
          {change.text}
        </div>
      )}
    </div>
  );
}
