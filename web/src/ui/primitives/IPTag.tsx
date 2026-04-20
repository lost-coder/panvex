import { useState } from "react";
import { cn } from "@/ui/lib/cn";

export interface IPTagProps {
  address: string;
  className?: string;
}

export function IPTag({ address, className }: IPTagProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(address).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };

  return (
    <button
      type="button"
      onClick={handleCopy}
      className={cn(
        "inline-flex items-center gap-1 rounded bg-fg-faint px-1.5 py-0.5 font-mono text-caption transition-colors",
        "hover:bg-bg-hover hover:text-fg",
        "active:scale-[0.97]",
        className,
      )}
      title="Copy to clipboard"
    >
      {address}
      <span className="text-[9px] opacity-60">{copied ? "✓" : "⎘"}</span>
    </button>
  );
}
