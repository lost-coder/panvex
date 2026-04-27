import { useState } from "react";
import { Copy, Check } from "lucide-react";
import { cn } from "@/ui/lib/cn";

export interface CopyButtonProps {
  text: string;
  className?: string;
}

export function CopyButton({ text, className }: Readonly<CopyButtonProps>) {
  const [copied, setCopied] = useState(false);
  const handleCopy = () => {
    const done = () => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    };
    // The async Clipboard API is the only modern path; the
    // document.execCommand("copy") fallback was deprecated and removed
    // from every browser we ship to. Quietly no-op in environments
    // where navigator.clipboard is unavailable (older WebViews) so the
    // UI stays usable even if the copy never lands.
    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(text).then(done).catch(() => {});
    } else {
      done();
    }
  };
  return (
    <button
      type="button"
      onClick={handleCopy}
      className={cn(
        "ml-1 p-0.5 rounded hover:bg-bg-hover transition-colors text-fg-muted hover:text-fg",
        className,
      )}
      title="Copy"
    >
      {copied ? <Check className="w-3 h-3 text-status-ok" /> : <Copy className="w-3 h-3" />}
    </button>
  );
}
