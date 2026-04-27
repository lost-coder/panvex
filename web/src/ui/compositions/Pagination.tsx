import { cn } from "@/ui/lib/cn";

export interface PaginationProps {
  page: number;
  totalPages: number;
  onPageChange: (page: number) => void;
  className?: string;
}

export function Pagination({ page, totalPages, onPageChange, className }: Readonly<PaginationProps>) {
  const pages = buildRange(page, totalPages);

  return (
    <nav
      aria-label="Pagination"
      className={cn("flex items-center justify-center gap-1", className)}
    >
      <PageBtn disabled={page <= 1} onClick={() => onPageChange(page - 1)}>
        ‹
      </PageBtn>
      {pages.map((p, i) =>
        p === null ? (
          <span key={`e${i}`} className="px-1 text-fg-muted/40 text-xs">
            …
          </span>
        ) : (
          <PageBtn key={p} active={p === page} onClick={() => onPageChange(p)}>
            {p}
          </PageBtn>
        ),
      )}
      <PageBtn disabled={page >= totalPages} onClick={() => onPageChange(page + 1)}>
        ›
      </PageBtn>
    </nav>
  );
}

function PageBtn({
  active,
  disabled,
  onClick,
  children,
}: Readonly<{
  active?: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}>) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "h-8 min-w-[32px] px-2 rounded-xs text-xs font-mono transition-colors",
        "disabled:opacity-30 disabled:cursor-not-allowed",
        active ? "bg-accent text-white" : "text-fg-muted hover:text-fg hover:bg-bg-hover",
      )}
    >
      {children}
    </button>
  );
}

function buildRange(page: number, total: number): (number | null)[] {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1);
  const pages: (number | null)[] = [1];
  const start = Math.max(2, page - 1);
  const end = Math.min(total - 1, page + 1);
  if (start > 2) pages.push(null);
  for (let i = start; i <= end; i++) pages.push(i);
  if (end < total - 1) pages.push(null);
  pages.push(total);
  return pages;
}
