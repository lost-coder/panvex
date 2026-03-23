import { ReactNode } from "react";
import { Search } from "lucide-react";
import { cn } from "@/lib/cn";

interface ToolbarProps {
  search?: {
    value: string;
    onChange: (v: string) => void;
    placeholder?: string;
  };
  filters?: ReactNode;
  viewToggle?: ReactNode;
  actions?: ReactNode;
}

export function Toolbar({ search, filters, viewToggle, actions }: ToolbarProps) {
  return (
    <div className={cn("flex items-center gap-2.5 mb-3.5 flex-wrap")}>
      {search && (
        <div className="flex-1 min-w-[200px] max-w-[320px] flex items-center gap-2 bg-card border border-border rounded-xs px-3 py-2 transition-all focus-within:border-accent">
          <Search className="w-[15px] h-[15px] text-text-3 shrink-0" />
          <input
            value={search.value}
            onChange={(e) => search.onChange(e.target.value)}
            placeholder={search.placeholder}
            className="border-none bg-transparent outline-none text-[13px] font-sans text-text-1 w-full placeholder:text-text-4"
          />
        </div>
      )}
      {filters}
      <div className="flex-1" />
      {viewToggle}
      {actions}
    </div>
  );
}
