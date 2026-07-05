// Subscription link card: shows the per-client subscription URL with a
// copy button and a rotate-token action. Mirrors the SecretSection visual
// idiom (raw <section> + Button from @/ui) so the card fits seamlessly
// into the ClientDetailPage layout.

import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/ui";
import { writeClipboard } from "@/ui/lib/clipboard";

export interface SubscriptionLinkCardProps {
  url: string;
  rotating: boolean;
  onRotate: () => void;
}

export function SubscriptionLinkCard({
  url,
  rotating,
  onRotate,
}: Readonly<SubscriptionLinkCardProps>) {
  const { t } = useTranslation("clients");
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    void writeClipboard(url).then((ok) => {
      if (!ok) return;
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };

  const handleRotate = () => {
    if (globalThis.confirm(t("subscription.rotateConfirm"))) {
      onRotate();
    }
  };

  return (
    <section className="rounded-xs bg-bg-card border border-divider p-4 flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <span className="text-sm font-semibold text-fg">
          {t("subscription.title")}
        </span>
        {url ? (
          <Button
            size="sm"
            variant="outline"
            disabled={rotating}
            onClick={handleRotate}
          >
            {t("subscription.rotate")}
          </Button>
        ) : null}
      </div>
      {url ? (
        <div className="flex items-center gap-2 rounded-xs bg-bg border border-divider px-3 py-2 min-w-0">
          <code className="flex-1 min-w-0 text-sm font-mono text-fg break-all select-all">
            {url}
          </code>
          <Button
            size="sm"
            variant="ghost"
            onClick={handleCopy}
            className="shrink-0"
          >
            {copied ? t("subscription.copied") : t("subscription.copy")}
          </Button>
        </div>
      ) : (
        <div className="rounded-xs bg-bg border border-dashed border-divider px-3 py-2 text-micro font-mono text-fg-muted">
          {t("subscription.none")}
        </div>
      )}
    </section>
  );
}
