import { ReactNode } from "react";
import { cn } from "@/lib/cn";

export function SummaryStatsRow({ children }: { children: ReactNode }) {
  return (
    <div className={cn("grid grid-cols-2 md:grid-cols-4 gap-3")}>
      {children}
    </div>
  );
}
