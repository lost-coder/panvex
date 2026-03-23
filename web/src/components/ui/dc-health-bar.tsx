import { cn } from "@/lib/cn";

const segmentColors = {
  ok: "bg-good",
  partial: "bg-warn",
  down: "bg-bad",
};

interface DcHealthBarProps {
  segments: Array<"ok" | "partial" | "down">;
  size?: "default" | "mini";
}

export function DcHealthBar({ segments, size = "default" }: DcHealthBarProps) {
  return (
    <div className="flex gap-0.5">
      {segments.map((seg, i) => (
        <div
          key={i}
          className={cn(
            "rounded-sm",
            size === "default" ? "h-2 flex-1 min-w-[8px]" : "h-1 flex-1 min-w-[4px]",
            segmentColors[seg]
          )}
        />
      ))}
    </div>
  );
}
