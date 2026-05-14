import { useState, type ReactElement } from "react";
import { useTranslation } from "react-i18next";

interface TelemtUnreachableBannerProps {
  sinceUnix: number;
  /**
   * Optional override for "now" — supplied by tests to make the elapsed
   * duration deterministic. Production callers omit this and we fall
   * back to Date.now().
   */
  nowUnix?: number;
}

function formatHHMMSS(unix: number): string {
  if (unix <= 0) return "—";
  const d = new Date(unix * 1000);
  const hh = String(d.getUTCHours()).padStart(2, "0");
  const mm = String(d.getUTCMinutes()).padStart(2, "0");
  const ss = String(d.getUTCSeconds()).padStart(2, "0");
  return `${hh}:${mm}:${ss}`;
}

export function TelemtUnreachableBanner(
  props: TelemtUnreachableBannerProps,
): ReactElement {
  const { t } = useTranslation("servers");
  const { sinceUnix, nowUnix } = props;
  // Date.now() is impure and would violate the react-compiler "no impure
  // calls during render" rule; wrap it in a lazy useState initializer so
  // the value is captured once at mount and stays stable across rerenders.
  const [fallbackNow] = useState(() => Date.now() / 1000);
  const now = nowUnix ?? fallbackNow;
  const elapsed = sinceUnix > 0 ? now - sinceUnix : 0;
  const sinceText = formatHHMMSS(sinceUnix);
  const formatElapsed = (seconds: number): string => {
    if (seconds < 60) return t("detail.elapsedSec", { value: Math.max(0, Math.floor(seconds)) });
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return t("detail.elapsedMin", { value: minutes });
    const hours = Math.floor(minutes / 60);
    const restMin = minutes % 60;
    return restMin === 0
      ? t("detail.elapsedHour", { value: hours })
      : t("detail.elapsedHourMin", { hours, minutes: restMin });
  };
  const elapsedText = formatElapsed(elapsed);
  return (
    <div
      role="alert"
      className="rounded-md border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-100"
    >
      <div className="font-semibold">{t("detail.telemtLost")}</div>
      <div className="text-red-200/80">
        {t("detail.telemtLostDetail", { since: sinceText, elapsed: elapsedText })}
      </div>
    </div>
  );
}
