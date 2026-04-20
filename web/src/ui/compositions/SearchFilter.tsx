import { cn } from "@/ui/lib/cn";
import { Input } from "@/ui/base/input";
import { Select, type SelectOption } from "@/ui/base/select";

export interface SearchFilterProps {
  searchValue: string;
  onSearchChange: (value: string) => void;
  searchPlaceholder?: string;
  filters?: {
    key: string;
    options: SelectOption[];
    value?: string;
    onChange: (value: string) => void;
    placeholder?: string;
  }[];
  className?: string;
}

export function SearchFilter({
  searchValue,
  onSearchChange,
  searchPlaceholder = "Search…",
  filters,
  className,
}: SearchFilterProps) {
  return (
    <div className={cn("flex flex-col gap-2 sm:flex-row sm:items-center", className)}>
      <Input
        value={searchValue}
        onChange={(e) => onSearchChange(e.target.value)}
        placeholder={searchPlaceholder}
        className="flex-1 sm:max-w-[280px]"
      />
      {filters?.map((filter) => (
        <Select
          key={filter.key}
          options={filter.options}
          value={filter.value}
          onChange={filter.onChange}
          placeholder={filter.placeholder}
          className="sm:w-[160px]"
        />
      ))}
    </div>
  );
}
