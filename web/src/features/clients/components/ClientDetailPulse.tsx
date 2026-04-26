// R-Q-08: pulse-row + percentage/tone math extracted from
// ClientDetailPage.tsx. Pure: takes the raw client snapshot and the
// expiry helpers from `clientDetailHelpers` and renders the four-tile
// strip used on both mobile and desktop layouts.

import { PulseRow, formatBytes, formatExpiry, formatQuota, type PulseTick } from "@/ui";
import type { ClientDetailPageProps } from "@/shared/api/types-pages/pages";

import { expiresSuffix, expiresTone } from "./clientDetailHelpers";

type Client = ClientDetailPageProps["client"];

function ratioTone(
  pct: number | undefined,
  okBelow80: boolean,
): "default" | "ok" | "warn" | "error" {
  if (typeof pct !== "number") return "default";
  if (pct >= 100) return "error";
  if (pct >= 80) return "warn";
  return okBelow80 ? "ok" : "default";
}

export function ClientDetailPulse({ client }: { client: Client }) {
  const trafficPct = client.dataQuotaBytes
    ? Math.min(100, (client.trafficUsedBytes / client.dataQuotaBytes) * 100)
    : undefined;
  const connsPct =
    client.maxTcpConns > 0 ? (client.activeTcpConns / client.maxTcpConns) * 100 : undefined;
  const ipsPct =
    client.maxUniqueIps > 0 ? (client.uniqueIpsUsed / client.maxUniqueIps) * 100 : undefined;
  return (
    <PulseRow
      ticks={
        [
          {
            label: "Connections",
            value: client.activeTcpConns.toLocaleString(),
            hint:
              client.maxTcpConns > 0
                ? `of ${client.maxTcpConns.toLocaleString()} max`
                : "no limit",
            tone: ratioTone(connsPct, false),
            barPct: connsPct,
          },
          {
            label: "Unique IPs",
            value: client.uniqueIpsUsed.toLocaleString(),
            hint:
              client.maxUniqueIps > 0
                ? `of ${client.maxUniqueIps.toLocaleString()} max`
                : "no limit",
            tone: ratioTone(ipsPct, false),
            barPct: ipsPct,
          },
          {
            label: "Traffic",
            value: formatBytes(client.trafficUsedBytes),
            hint:
              client.dataQuotaBytes > 0
                ? `of ${formatQuota(client.dataQuotaBytes)}`
                : "no quota",
            tone: ratioTone(trafficPct, true),
            barPct: trafficPct,
          },
          {
            label: "Expires",
            value: formatExpiry(client.expirationRfc3339),
            hint: expiresSuffix(client.expirationRfc3339),
            tone: expiresTone(client.expirationRfc3339),
          },
        ] satisfies PulseTick[]
      }
    />
  );
}
