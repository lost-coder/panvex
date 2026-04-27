import { cn } from "@/ui/lib/cn";
import { DCCard, type DCCardProps } from "@/features/servers/ui/DCCard";

export interface DCScrollStripProps {
  items: DCCardProps[];
  onSelect?: (code: string) => void;
  /** Desktop grid columns: [md, xl]. Default: [4, 6] */
  cols?: [number, number];
  className?: string;
}

const mdColsClass: Record<number, string> = {
  1: "md:grid-cols-1",
  2: "md:grid-cols-2",
  3: "md:grid-cols-3",
  4: "md:grid-cols-4",
  5: "md:grid-cols-5",
  6: "md:grid-cols-6",
};
const xlColsClass: Record<number, string> = {
  1: "xl:grid-cols-1",
  2: "xl:grid-cols-2",
  3: "xl:grid-cols-3",
  4: "xl:grid-cols-4",
  5: "xl:grid-cols-5",
  6: "xl:grid-cols-6",
};

export function DCScrollStrip({ items, onSelect, cols = [4, 6], className }: Readonly<DCScrollStripProps>) {
  return (
    <div
      className={cn(
        "flex gap-2 overflow-x-auto pb-2 snap-x snap-mandatory scrollbar-thin",
        "md:grid md:overflow-visible md:snap-none md:pb-0",
        mdColsClass[cols[0]],
        xlColsClass[cols[1]],
        className,
      )}
    >
      {items.map((item) => (
        <DCCard
          key={item.code}
          {...item}
          onClick={() => onSelect?.(item.code)}
          className="shrink-0 md:shrink"
        />
      ))}
    </div>
  );
}
