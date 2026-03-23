import { ReactNode } from "react";
import { cn } from "@/lib/cn";

interface EmptyMessageProps {
  icon?: ReactNode;
  title: string;
  description?: string;
}

export function EmptyMessage({ icon, title, description }: EmptyMessageProps) {
  return (
    <div className={cn("flex flex-col items-center justify-center gap-2 py-10 text-center")}>
      {icon && <div className="text-text-4 mb-1">{icon}</div>}
      <p className="text-[14px] font-semibold text-text-3">{title}</p>
      {description && (
        <p className="text-[12px] text-text-4 max-w-[240px]">{description}</p>
      )}
    </div>
  );
}
