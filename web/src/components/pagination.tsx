import { ChevronLeft, ChevronRight } from "lucide-react";
import { cn } from "@/lib/cn";

interface PaginationProps {
  page: number;
  totalPages: number;
  onPage: (p: number) => void;
}

export function Pagination({ page, totalPages, onPage }: PaginationProps) {
  if (totalPages <= 1) return null;
  return (
    <div className="flex items-center gap-1 justify-end mt-3">
      <button
        onClick={() => onPage(page - 1)}
        disabled={page <= 1}
        className={cn("p-1.5 rounded-xs text-text-3 hover:text-text-1 hover:bg-input transition-all", page <= 1 && "opacity-40 cursor-not-allowed")}
      >
        <ChevronLeft className="w-4 h-4" />
      </button>
      <span className="text-xs text-text-2 px-2">{page} / {totalPages}</span>
      <button
        onClick={() => onPage(page + 1)}
        disabled={page >= totalPages}
        className={cn("p-1.5 rounded-xs text-text-3 hover:text-text-1 hover:bg-input transition-all", page >= totalPages && "opacity-40 cursor-not-allowed")}
      >
        <ChevronRight className="w-4 h-4" />
      </button>
    </div>
  );
}
