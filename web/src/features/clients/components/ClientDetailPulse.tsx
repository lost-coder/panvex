// R-Q-08: pulse-row + percentage/tone math extracted from
// ClientDetailPage.tsx. Pure: takes the raw client snapshot and the
// expiry helpers from `clientDetailHelpers` and renders the four-tile
// strip used on both mobile and desktop layouts.

import { useTranslation } from "react-i18next";

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

export function ClientDetailPulse({ client }: Readonly<{ client: Client }>) {
  const { t } = useTranslation("clients");
  // Limits are per-Telemt-node; usage is summed across deployments.
  // Multiply the per-node limit by deployment count so the ratio
  // compares like with like (otherwise a 4-node client with 50/conn
  // looks 100% saturated at 50 conns while every node still has room).
  const nodes = Math.max(1, client.deployments.length);
  const effectiveQuota = client.dataQuotaBytes * nodes;
  const effectiveConns = client.maxTcpConns * nodes;
  const effectiveIps = client.maxUniqueIps * nodes;

  const trafficPct = effectiveQuota
    ? Math.min(100, (client.trafficUsedBytes / effectiveQuota) * 100)
    : undefined;
  const connsPct =
    effectiveConns > 0 ? (client.activeTcpConns / effectiveConns) * 100 : undefined;
  const ipsPct =
    effectiveIps > 0 ? (client.uniqueIpsUsed / effectiveIps) * 100 : undefined;
  return (
    <PulseRow
      ticks={
        [
          {
            label: t("pulseDetail.connections"),
            value: client.activeTcpConns.toLocaleString(),
            hint:
              effectiveConns > 0
                ? t("pulseDetail.connectionsHint", { max: effectiveConns.toLocaleString() })
                : t("pulseDetail.noLimit"),
            tone: ratioTone(connsPct, false),
            barPct: connsPct,
          },
          {
            label: t("pulseDetail.uniqueIps"),
            value: client.uniqueIpsUsed.toLocaleString(),
            hint:
              effectiveIps > 0
                ? t("pulseDetail.uniqueIpsHint", { max: effectiveIps.toLocaleString() })
                : t("pulseDetail.noLimit"),
            tone: ratioTone(ipsPct, false),
            barPct: ipsPct,
          },
          {
            label: t("pulseDetail.traffic"),
            value: formatBytes(client.trafficUsedBytes),
            hint:
              effectiveQuota > 0
                ? t("pulseDetail.trafficHint", { max: formatQuota(effectiveQuota) })
                : t("pulseDetail.noQuota"),
            tone: ratioTone(trafficPct, true),
            barPct: trafficPct,
          },
          {
            label: t("pulseDetail.expires"),
            value: formatExpiry(client.expirationRfc3339),
            hint: expiresSuffix(client.expirationRfc3339),
            tone: expiresTone(client.expirationRfc3339),
          },
        ] satisfies PulseTick[]
      }
    />
  );
}
