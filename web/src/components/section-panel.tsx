import { ReactNode } from "react";
import { cn } from "@/lib/cn";

interface SectionPanelProps {
  title: string;
  icon?: ReactNode;
  headerRight?: ReactNode;
  children: ReactNode;
  className?: string;
}

export function SectionPanel({ title, icon, headerRight, children, className }: SectionPanelProps) {
  return (
    <div className={cn("bg-card border border-border rounded backdrop-blur-[var(--blur)] overflow-hidden", className)}>
      <div className="flex items-center justify-between px-4 py-3 border-b border-border">
        <div className="flex items-center gap-2">
          {icon && <span className="w-4 h-4 text-accent">{icon}</span>}
          <span className="text-[13px] font-bold text-text-1">{title}</span>
        </div>
        {headerRight && <div>{headerRight}</div>}
      </div>
      {children}
    </div>
  );
}
