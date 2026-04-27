import { cn } from "@/ui/lib/cn";
import { GaugeCell, type GaugeCellProps } from "@/ui/primitives/GaugeCell";

export interface GaugeStripProps {
  items: GaugeCellProps[];
  /** Grid columns: [mobile, desktop]. Default: [2, 4] */
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

export function GaugeStrip({ items, cols = [2, 4], className }: Readonly<GaugeStripProps>) {
  return (
    <div className={cn("grid gap-2", colsClass[cols[0]], mdColsClass[cols[1]], className)}>
      {items.map((item) => (
        <GaugeCell key={item.label} {...item} />
      ))}
    </div>
  );
}
