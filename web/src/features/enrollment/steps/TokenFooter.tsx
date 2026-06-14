import { useTranslation } from "react-i18next";

import { Button } from "@/ui/base/button";

interface TokenFooterProps {
  tokenValue: string;
  /** Live remaining TTL in seconds; <= 0 means expired. */
  remainingSecs: number;
  /** When provided, the expired state offers a "New token" button. */
  onRegenerate?: (() => void) | undefined;
}

/**
 * Shared "Token: …  Expires in: N min" footer for wizard steps 2/3.
 * The parent feeds a LIVE remainingSecs (AddServerContainer derives it
 * from useNowSec), so the countdown actually counts down — the audit's
 * E1 finding was a frozen value captured at token-mint time.
 */
export function TokenFooter({ tokenValue, remainingSecs, onRegenerate }: Readonly<TokenFooterProps>) {
  const { t } = useTranslation("enrollment");
  const expired = remainingSecs <= 0;
  const minutes = Math.max(1, Math.ceil(remainingSecs / 60));

  return (
    <div className="flex items-center justify-between gap-2 text-xs text-fg-muted rounded-xs bg-bg-card border border-divider px-3 py-2">
      <span>
        {t("tokenFooter.token")}{" "}
        <span className="font-mono">{tokenValue.slice(0, 13)}…</span>
      </span>
      {expired ? (
        <span className="flex items-center gap-2">
          <span className="text-status-error">{t("tokenFooter.expired")}</span>
          {onRegenerate && (
            <Button size="sm" variant="ghost" onClick={onRegenerate}>
              {t("tokenFooter.regenerate")}
            </Button>
          )}
        </span>
      ) : (
        <span>
          {t("tokenFooter.expiresIn")}{" "}
          <span className="text-status-warn">
            {t("tokenFooter.minutes", { count: minutes })}
          </span>
        </span>
      )}
    </div>
  );
}
