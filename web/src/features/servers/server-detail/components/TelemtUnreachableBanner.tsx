import type { ReactElement } from "react";

interface TelemtUnreachableBannerProps {
  sinceUnix: number;
  /**
   * Optional override for "now" — supplied by tests to make the elapsed
   * duration deterministic. Production callers omit this and we fall
   * back to Date.now().
   */
  nowUnix?: number;
}

function formatElapsed(seconds: number): string {
  if (seconds < 60) return `${Math.max(0, Math.floor(seconds))} с`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} мин`;
  const hours = Math.floor(minutes / 60);
  const restMin = minutes % 60;
  return restMin === 0 ? `${hours} ч` : `${hours} ч ${restMin} мин`;
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
  const { sinceUnix, nowUnix } = props;
  const now = nowUnix ?? Date.now() / 1000;
  const elapsed = sinceUnix > 0 ? now - sinceUnix : 0;
  const sinceText = formatHHMMSS(sinceUnix);
  const elapsedText = formatElapsed(elapsed);
  return (
    <div
      role="alert"
      className="rounded-md border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-100"
    >
      <div className="font-semibold">Связь с Telemt потеряна</div>
      <div className="text-red-200/80">
        с {sinceText} UTC ({elapsedText}). Проверьте, что Telemt запущен и слушает
        loopback. Метрики и режим работы недоступны до восстановления связи.
      </div>
    </div>
  );
}
