import { cn } from "@/lib/cn";

type ActivityStatus = "good" | "warn" | "bad" | "accent";

interface ActivityItem {
  id: string;
  text: string;
  time: string;
  status?: ActivityStatus;
}

const dotColor: Record<ActivityStatus, string> = {
  good: "bg-good",
  warn: "bg-warn",
  bad: "bg-bad",
  accent: "bg-accent",
};

export function ActivityFeed({
  items,
  emptyMessage = "No activity",
}: {
  items: ActivityItem[];
  emptyMessage?: string;
}) {
  if (items.length === 0) {
    return <p className="text-[13px] text-text-3 py-6 px-4">{emptyMessage}</p>;
  }
  return (
    <div className="px-4">
      {items.map((item) => (
        <div
          key={item.id}
          className="flex items-start gap-3 py-2.5 border-b border-border last:border-b-0"
        >
          <span
            className={cn(
              "w-2 h-2 rounded-full mt-1.5 shrink-0",
              dotColor[item.status ?? "accent"]
            )}
          />
          <span className="text-[13px] text-text-2 flex-1">{item.text}</span>
          <span className="text-[11px] text-text-4 shrink-0 whitespace-nowrap">{item.time}</span>
        </div>
      ))}
    </div>
  );
}
