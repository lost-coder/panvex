import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/ui/lib/cn";

export interface FallbackBannerProps {
  durationSeconds: number;
  escalated: boolean;
  enteredAtUnix?: number | null;
}

const TICK_MS = 30_000;

export function FallbackBanner({
  durationSeconds,
  escalated,
  enteredAtUnix = null,
}: Readonly<FallbackBannerProps>) {
  const { t } = useTranslation("servers");
  const [liveDurationSeconds, setLiveDurationSeconds] =
    useState(durationSeconds);

  useEffect(() => {
    setLiveDurationSeconds(durationSeconds);
    if (enteredAtUnix == null) return;
    const id = window.setInterval(() => {
      setLiveDurationSeconds(
        Math.max(0, Math.floor(Date.now() / 1000) - enteredAtUnix),
      );
    }, TICK_MS);
    return () => window.clearInterval(id);
  }, [durationSeconds, enteredAtUnix]);

  const severity = escalated ? "critical" : "warn";
  const headline = escalated
    ? t("detail.fallback.criticalHeadline")
    : t("detail.fallback.warnHeadline");
  const body = escalated
    ? t("detail.fallback.criticalBody")
    : t("detail.fallback.warnBody");

  return (
    <div
      data-severity={severity}
      className={cn(
        "rounded-md border p-3 text-sm",
        escalated
          ? "bg-status-error-soft border-status-error text-status-error"
          : "bg-status-warning-soft border-status-warning text-status-warning",
      )}
    >
      <strong>{headline}</strong>
      <p className="mt-1 text-fg">
        {body} {t("detail.fallback.activeFor", { minutes: Math.round(liveDurationSeconds / 60) })}
      </p>
    </div>
  );
}
