import { Input } from "@/ui/base/input";
import { cn } from "@/ui/lib/cn";

export interface FilterBarProps {
  /** Group of chips / tabs on the left. Render FilterChips here. */
  chips: React.ReactNode;
  /** Search input wired up via value+onChange. */
  search?: {
    value: string;
    onChange: (v: string) => void;
    placeholder?: string;
    className?: string;
  };
  /** Optional extra content between the chips and search (e.g. secondary
   *  chip row on mobile). If omitted, search aligns to the right on md+. */
  trailing?: React.ReactNode;
  className?: string;
}

/**
 * Horizontal filter bar: chip group on the left, search input right-aligned.
 * Collapses to a wrapping layout on narrow widths — the search input falls
 * below the chips rather than scrolling horizontally.
 */
export function FilterBar({ chips, search, trailing, className }: FilterBarProps) {
  return (
    <div className={cn("flex flex-wrap items-center gap-3", className)}>
      <div className="flex flex-wrap gap-1.5">{chips}</div>
      {trailing}
      {search && (
        <div className="ml-auto w-full sm:w-auto">
          <Input
            type="search"
            value={search.value}
            onChange={(e) => search.onChange(e.target.value)}
            placeholder={search.placeholder}
            className={cn("w-full sm:w-64", search.className)}
          />
        </div>
      )}
    </div>
  );
}
