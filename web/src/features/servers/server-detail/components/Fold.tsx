import { useState } from "react";
import { ChevronRight } from "lucide-react";

import { cn } from "@/ui";

// ─── Collapsible fold ─────────────────────────────────────────────────
export function Fold({
  title,
  rightHint,
  defaultOpen = true,
  children,
}: {
  title: string;
  rightHint?: string;
  defaultOpen?: boolean;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <section className="rounded-xs bg-bg-card border border-border overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-bg-hover transition-colors"
      >
        <ChevronRight
          className={cn("size-4 transition-transform", open && "rotate-90")}
          aria-hidden="true"
        />
        <span className="text-sm font-semibold text-fg">{title}</span>
        {rightHint && (
          <span className="ml-auto text-[11px] font-mono text-fg-muted">{rightHint}</span>
        )}
      </button>
      {open && <div className="px-4 py-4 border-t border-border">{children}</div>}
    </section>
  );
}
