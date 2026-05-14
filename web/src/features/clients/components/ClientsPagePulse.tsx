// R-Q-08: pulse strip + counts memo extracted from ClientsPage.tsx.
// Pure presentation: takes a precomputed counts object and renders the
// four-tile ribbon. The counts shape is exported so the host page can
// reuse it for chip labels.
//
// R-Q-24: pulse component + buildClientCounts helper co-located by design.
/* eslint-disable react-refresh/only-export-components */

import { useTranslation } from "react-i18next";

import { PulseRow, type ClientListItem, type PulseTick } from "@/ui";

import { effectiveClientStatus } from "./ClientsPageCells";

export interface ClientCounts {
  all: number;
  active: number;
  disabled: number;
  expired: number;
  online: number;
  quotaExhausted: number;
}

export function buildClientCounts(clients: ClientListItem[], nowMs: number): ClientCounts {
  let active = 0;
  let disabled = 0;
  let expired = 0;
  let online = 0;
  let quotaExhausted = 0;
  for (const c of clients) {
    const s = effectiveClientStatus(c, nowMs);
    if (s === "active") active++;
    else if (s === "disabled") disabled++;
    else expired++;
    if (c.activeTcpConns > 0) online++;
    // Quota is per-Telemt-node × deployment count (see LimitsCard).
    if (
      c.dataQuotaBytes > 0 &&
      c.trafficUsedBytes >= c.dataQuotaBytes * Math.max(1, c.assignedNodesCount)
    ) {
      quotaExhausted++;
    }
  }
  return { all: clients.length, active, disabled, expired, online, quotaExhausted };
}

export function ClientsPagePulse({ counts }: Readonly<{ counts: ClientCounts }>) {
  const { t } = useTranslation("clients");
  return (
    <PulseRow
      ticks={
        [
          {
            label: t("pulse.total"),
            value: counts.all.toLocaleString(),
            hint: t("pulse.totalHint", { count: counts.disabled }),
          },
          {
            label: t("pulse.activeNow"),
            value: counts.online.toLocaleString(),
            hint: t("pulse.activeNowHint"),
            tone: counts.online > 0 ? "ok" : "default",
          },
          {
            label: t("pulse.expired"),
            value: counts.expired.toLocaleString(),
            hint: counts.expired > 0 ? t("pulse.expiredHintWith") : t("pulse.expiredHintNone"),
            tone: counts.expired > 0 ? "error" : "default",
          },
          {
            label: t("pulse.quotaExhausted"),
            value: counts.quotaExhausted.toLocaleString(),
            hint:
              counts.quotaExhausted > 0
                ? t("pulse.quotaExhaustedHintWith")
                : t("pulse.quotaExhaustedHintNone"),
            tone: counts.quotaExhausted > 0 ? "warn" : "default",
          },
        ] satisfies PulseTick[]
      }
    />
  );
}
