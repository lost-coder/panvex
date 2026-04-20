import React from "react";
import { cn } from "@/ui/lib/cn";
import { FieldLabel } from "./FieldLabel";

export interface StatCardProps {
  label: string;
  value: React.ReactNode;
  className?: string;
}

export function StatCard({ label, value, className }: StatCardProps) {
  return (
    <div className={cn("rounded-xs bg-bg-card p-3 flex flex-col gap-0.5", className)}>
      <span className="text-lg font-mono font-semibold text-fg leading-none">{value}</span>
      <FieldLabel>{label}</FieldLabel>
    </div>
  );
}
