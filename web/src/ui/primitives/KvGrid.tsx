import React from "react";
import { cn } from "@/ui/lib/cn";

export interface KvGridRow {
  label: string;
  value: React.ReactNode;
}

export interface KvGridProps {
  rows: KvGridRow[];
  className?: string;
}

export function KvGrid({ rows, className }: KvGridProps) {
  return (
    <div className={cn("grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm", className)}>
      {rows.map(({ label, value }) => (
        <React.Fragment key={label}>
          <span className="text-fg-muted">{label}</span>
          <span className="font-mono text-xs text-fg">{value}</span>
        </React.Fragment>
      ))}
    </div>
  );
}
