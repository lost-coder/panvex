import { useState } from "react";
import { Copy, Check } from "lucide-react";
import { cn } from "@/ui/lib/cn";

export interface CopyButtonProps {
  text: string;
  className?: string;
}

function fallbackCopy(value: string) {
  const ta = document.createElement("textarea");
  ta.value = value;
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  document.execCommand("copy");
  document.body.removeChild(ta);
}

export function CopyButton({ text, className }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);
  const handleCopy = () => {
    const done = () => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    };
    if (navigator.clipboard?.writeText) {
      void navigator.clipboard.writeText(text).then(done);
    } else {
      fallbackCopy(text);
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
