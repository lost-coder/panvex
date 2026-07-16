import type { ReactElement } from "react";
import { useTranslation } from "react-i18next";

/**
 * Warns that Telemt on this node is not exporting per-user series, so the
 * per-client traffic and quota-usage figures elsewhere on the page are stale
 * or zero. Either user telemetry is switched off in Telemt's config, or the
 * node exceeded Telemt's 4096-user labeled-series cap.
 *
 * Deliberately non-blocking, unlike TelemtUnreachableBanner: the node itself
 * is fine and the rest of the page stays useful, so this renders as a warning
 * ABOVE the content rather than replacing it, and uses role="status" (polite)
 * instead of role="alert" (assertive).
 */
export function UserTelemetrySuppressedBanner(): ReactElement {
  const { t } = useTranslation("servers");
  return (
    <div
      role="status"
      className="rounded-md border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-100"
    >
      <div className="font-semibold">{t("detail.userTelemetrySuppressed")}</div>
      <div className="text-amber-200/80">{t("detail.userTelemetrySuppressedDetail")}</div>
    </div>
  );
}
