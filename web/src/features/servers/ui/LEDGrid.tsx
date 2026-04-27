import { cn } from "@/ui/lib/cn";
import { LEDIndicator, type LEDIndicatorProps } from "@/ui/primitives/LEDIndicator";

export interface LEDGridProps {
  items: LEDIndicatorProps[];
  activeCode?: string;
  onSelect?: (label: string) => void;
  /** Grid columns: [mobile, desktop]. Default: [4, 6] */
  cols?: [number, number];
  className?: string;
}

const colsClass: Record<number, string> = {
  1: "grid-cols-1",
  2: "grid-cols-2",
  3: "grid-cols-3",
  4: "grid-cols-4",
  5: "grid-cols-5",
  6: "grid-cols-6",
};
const mdColsClass: Record<number, string> = {
  1: "md:grid-cols-1",
  2: "md:grid-cols-2",
  3: "md:grid-cols-3",
  4: "md:grid-cols-4",
  5: "md:grid-cols-5",
  6: "md:grid-cols-6",
};

export function LEDGrid({ items, activeCode, onSelect, cols = [4, 6], className }: Readonly<LEDGridProps>) {
  return (
    <div
      role="group"
      aria-label="Data center status indicators"
      className={cn("grid gap-1", colsClass[cols[0]], mdColsClass[cols[1]], className)}
    >
      {items.map((item) => (
        <LEDIndicator
          key={item.label}
          {...item}
          active={item.label === activeCode}
          onClick={() => onSelect?.(item.label)}
        />
      ))}
    </div>
  );
}
