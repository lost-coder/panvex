import type { TFunction } from "i18next";
import type { PulseTickData } from "./components/PulseGrid";

function badRateTone(rate: number): "error" | "warn" | "default" {
  if (rate > 5) return "error";
  if (rate > 1) return "warn";
  return "default";
}

function coverageTone(pct: number): "error" | "warn" | "ok" {
  if (pct < 95) return "error";
  if (pct < 100) return "warn";
  return "ok";
}

/** Primitive inputs the pulse ribbon reads. Kept flat (not the raw
 *  `connections`/`summary` objects) so memo dependency lists can be
 *  value-level and exhaustive-deps stays happy. */
export interface PulseInputs {
  current: number;
  currentMe: number;
  currentDirect: number;
  activeUsers: number;
  connectionsTotal: number;
  configuredUsers: number;
  connectionsBadTotal: number;
  badRate: number;
  avgCoverage: number;
  minCoverage: number;
  dcOk: number;
  dcWarn: number;
  dcErr: number;
}

/**
 * Single source of truth for the pulse ribbon cells. The desktop and
 * mobile variants differ only in which hint strings they pick (full vs.
 * the shorter `*Mobile` variants), so the two layouts share this builder
 * instead of carrying near-duplicate inline arrays.
 */
export function buildPulseItems(
  t: TFunction,
  variant: "desktop" | "mobile",
  p: Readonly<PulseInputs>,
): PulseTickData[] {
  const mobile = variant === "mobile";
  return [
    {
      label: t("detail.pulse.connections"),
      value: p.current.toLocaleString(),
      hint: mobile
        ? t("detail.pulse.connectionsHintMobile", {
            me: p.currentMe.toLocaleString(),
            direct: p.currentDirect.toLocaleString(),
          })
        : t("detail.pulse.connectionsHint", {
            me: p.currentMe.toLocaleString(),
            direct: p.currentDirect.toLocaleString(),
            total: p.connectionsTotal.toLocaleString(),
          }),
    },
    {
      label: t("detail.pulse.activeUsers"),
      value: p.activeUsers.toLocaleString(),
      hint: mobile
        ? t("detail.pulse.activeUsersHintMobile", {
            configured: p.configuredUsers.toLocaleString(),
          })
        : t("detail.pulse.activeUsersHint", {
            configured: p.configuredUsers.toLocaleString(),
          }),
    },
    {
      label: t("detail.pulse.badRate"),
      value: `${p.badRate.toFixed(2)}%`,
      hint: mobile
        ? t("detail.pulse.badRateHintMobile", {
            bad: p.connectionsBadTotal.toLocaleString(),
          })
        : t("detail.pulse.badRateHint", {
            bad: p.connectionsBadTotal.toLocaleString(),
            total: p.connectionsTotal.toLocaleString(),
          }),
      tone: badRateTone(p.badRate),
    },
    {
      label: t("detail.pulse.coverage"),
      value: p.avgCoverage,
      unit: "%",
      hint: mobile
        ? t("detail.pulse.coverageHintMobile", {
            min: p.minCoverage,
            ok: p.dcOk,
            warn: p.dcWarn,
            err: p.dcErr,
          })
        : t("detail.pulse.coverageHint", {
            min: p.minCoverage,
            ok: p.dcOk,
            warn: p.dcWarn,
            err: p.dcErr,
          }),
      tone: coverageTone(p.avgCoverage),
    },
  ];
}
