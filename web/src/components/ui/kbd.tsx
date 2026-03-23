import type { ReactNode } from "react";

interface KbdProps {
  children: ReactNode;
}

export function Kbd({ children }: KbdProps) {
  return (
    <kbd className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-mono font-semibold bg-[rgba(255,255,255,0.06)] text-text-3 border border-border">
      {children}
    </kbd>
  );
}
