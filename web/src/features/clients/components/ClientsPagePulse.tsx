// R-Q-08: pulse strip + counts memo extracted from ClientsPage.tsx.
// Pure presentation: takes a precomputed counts object and renders the
// four-tile ribbon. The counts shape is exported so the host page can
// reuse it for chip labels.
//
// R-Q-24: pulse component + buildClientCounts helper co-located by design.
/* eslint-disable react-refresh/only-export-components */

import { useTranslation } from "react-i18next";

import { PulseRow, type ClientListItem, type PulseTick } from "@/ui";

import { deriveClientState } from "./ClientsPageCells";

export interface ClientCounts {
  all: number;
  active: number;
  expiring: number;
  expired: number;
  overQuota: number;
  disabled: number;
  notDeployed: number;
  deployFailed: number;
  online: number;
  quotaExhausted: number;
}

export function buildClientCounts(clients: ClientListItem[], nowMs: number): ClientCounts {
  const c: ClientCounts = {
    all: clients.length, active: 0, expiring: 0, expired: 0, overQuota: 0,
    disabled: 0, notDeployed: 0, deployFailed: 0, online: 0, quotaExhausted: 0,
  };
  for (const client of clients) {
    switch (deriveClientState(client, nowMs)) {
      case "active": c.active++; break;
      case "expiring": c.expiring++; break;
      case "expired": c.expired++; break;
      case "over_quota": c.overQuota++; break;
      case "disabled": c.disabled++; break;
      case "not_deployed": c.notDeployed++; break;
      case "deploy_failed": c.deployFailed++; break;
    }
    if (client.activeTcpConns > 0) c.online++;
    // quotaExhausted is an independent tally of ALL over-quota clients,
    // regardless of lifecycle state — it differs from the `overQuota` STATE
    // count (which only fires when a client isn't already expired/disabled/
    // deploy_failed, since deriveClientState returns the higher-priority state
    // first). The pulse wants the raw "how many are blowing quota" number.
    // Quota is per-Telemt-node × deployment count (see LimitsCard).
    if (
      client.dataQuotaBytes > 0 &&
      client.trafficUsedBytes >= client.dataQuotaBytes * Math.max(1, client.assignedNodesCount)
    ) {
      c.quotaExhausted++;
    }
  }
  return c;
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
