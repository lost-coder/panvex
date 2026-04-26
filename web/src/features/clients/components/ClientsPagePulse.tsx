// R-Q-08: pulse strip + counts memo extracted from ClientsPage.tsx.
// Pure presentation: takes a precomputed counts object and renders the
// four-tile ribbon. The counts shape is exported so the host page can
// reuse it for chip labels.
//
// R-Q-24: pulse component + buildClientCounts helper co-located by design.
/* eslint-disable react-refresh/only-export-components */

import { PulseRow, type PulseTick } from "@/ui";

import { effectiveClientStatus } from "./ClientsPageCells";
import type { ClientListItem } from "@/ui";

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
    if (c.dataQuotaBytes > 0 && c.trafficUsedBytes >= c.dataQuotaBytes) quotaExhausted++;
  }
  return { all: clients.length, active, disabled, expired, online, quotaExhausted };
}

export function ClientsPagePulse({ counts }: { counts: ClientCounts }) {
  return (
    <PulseRow
      ticks={
        [
          {
            label: "Total",
            value: counts.all.toLocaleString(),
            hint: `${counts.disabled.toLocaleString()} disabled`,
          },
          {
            label: "Active now",
            value: counts.online.toLocaleString(),
            hint: "holding connections",
            tone: counts.online > 0 ? "ok" : "default",
          },
          {
            label: "Expired",
            value: counts.expired.toLocaleString(),
            hint: counts.expired > 0 ? "past expiration date" : "none past expiry",
            tone: counts.expired > 0 ? "error" : "default",
          },
          {
            label: "Quota exhausted",
            value: counts.quotaExhausted.toLocaleString(),
            hint: counts.quotaExhausted > 0 ? "traffic ≥ quota" : "all within limits",
            tone: counts.quotaExhausted > 0 ? "warn" : "default",
          },
        ] satisfies PulseTick[]
      }
    />
  );
}
