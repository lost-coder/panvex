import { cn } from "@/lib/cn";

interface FilterChipProps {
  label: string;
  active?: boolean;
  count?: number;
  onClick?: () => void;
}

export function FilterChip({ label, active, count, onClick }: FilterChipProps) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-semibold font-sans cursor-pointer border transition-all",
        active
          ? "bg-accent-dim border-accent text-accent-bright"
          : "bg-card border-border text-text-2 hover:border-border-hover hover:text-text-1"
      )}
    >
      {label}
      {count !== undefined && (
        <span className="min-w-[18px] h-[18px] rounded-full bg-accent/20 text-accent-bright text-[10px] font-bold flex items-center justify-center">
          {count}
        </span>
      )}
    </button>
  );
}
