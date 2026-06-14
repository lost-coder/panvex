import { useState } from "react";
import { Copy, Check } from "lucide-react";
import { useTranslation } from "react-i18next";

import { cn } from "@/ui/lib/cn";

export interface CopyButtonProps {
  text: string;
  className?: string;
  /** Accessible name override; defaults to the localized "Copy". */
  label?: string;
}

export function CopyButton({ text, className, label }: Readonly<CopyButtonProps>) {
  const { t } = useTranslation("common");
  const [copied, setCopied] = useState(false);
  const name = label ?? t("copy.copy");

  const handleCopy = () => {
    const done = () => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    };
    // The async Clipboard API is the only modern path; quietly no-op in
    // environments where navigator.clipboard is unavailable (older
    // WebViews) so the UI stays usable even if the copy never lands.
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
      aria-label={name}
      title={name}
      className={cn(
        // Visual footprint stays compact; the ::after inset expands the
        // hit area to >=44px (audit E5 touch-target) without shifting
        // the surrounding inline layout.
        "relative ml-1 p-1 rounded hover:bg-bg-hover transition-colors text-fg-muted hover:text-fg",
        "after:absolute after:-inset-3 after:content-['']",
        className,
      )}
    >
      {copied ? (
        <Check className="w-3.5 h-3.5 text-status-ok" aria-hidden="true" />
      ) : (
        <Copy className="w-3.5 h-3.5" aria-hidden="true" />
      )}
      <span role="status" aria-live="polite" className="sr-only">
        {copied ? t("copy.copied") : ""}
      </span>
    </button>
  );
}
