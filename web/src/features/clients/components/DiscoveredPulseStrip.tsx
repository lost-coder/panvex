// R-Q-08: 4-tile pulse strip for /clients/discovered, extracted from
// DiscoveredClientsPage.tsx. Pure presentation, takes precomputed
// counts.
//
// R-Q-24: pulse component + filter-spec factory co-located by design.
/* eslint-disable react-refresh/only-export-components */

import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";

import { DiscoveredPulseCell } from "./DiscoveredMobileRow";

export interface DiscoveredCounts {
  all: number;
  pending: number;
  adopted: number;
  ignored: number;
  conflicts: number;
}

export function DiscoveredPulseStrip({ counts }: Readonly<{ counts: DiscoveredCounts }>) {
  const { t } = useTranslation("clients");
  return (
    <section className="rounded-xs bg-bg-card border border-border grid grid-cols-2 md:grid-cols-4">
      <DiscoveredPulseCell i={0} label={t("discovered.pulse.pending")} value={counts.pending} tone="warn" />
      <DiscoveredPulseCell i={1} label={t("discovered.pulse.adopted")} value={counts.adopted} tone="ok" />
      <DiscoveredPulseCell
        i={2}
        label={t("discovered.pulse.ignored")}
        value={counts.ignored}
        tone="default"
      />
      <DiscoveredPulseCell
        i={3}
        label={t("discovered.pulse.conflicts")}
        value={counts.conflicts}
        tone={counts.conflicts > 0 ? "error" : "default"}
      />
    </section>
  );
}

export interface DiscoveredFilterOptions {
  status: { value: string; onChange: (next: string) => void };
  conflicts: { value: string; onChange: (next: string) => void };
  counts: DiscoveredCounts;
  t: TFunction<"clients">;
}

export function buildDiscoveredFilters(opts: Readonly<DiscoveredFilterOptions>) {
  const { t } = opts;
  return [
    {
      key: "status",
      value: opts.status.value,
      onChange: opts.status.onChange,
      variant: "chips" as const,
      options: [
        { value: "all", label: t("discovered.filters.all", { count: opts.counts.all }) },
        {
          value: "pending",
          label: t("discovered.filters.pending", { count: opts.counts.pending }),
          tone: "warn" as const,
        },
        {
          value: "adopted",
          label: t("discovered.filters.adopted", { count: opts.counts.adopted }),
          tone: "ok" as const,
        },
        {
          value: "ignored",
          label: t("discovered.filters.ignored", { count: opts.counts.ignored }),
        },
      ],
      placeholder: t("discovered.filters.statusPlaceholder"),
    },
    {
      key: "conflicts",
      value: opts.conflicts.value,
      onChange: opts.conflicts.onChange,
      variant: "chips" as const,
      options: [
        { value: "all", label: t("discovered.filters.anyConflict") },
        {
          value: "only",
          label: t("discovered.filters.onlyConflicts", { count: opts.counts.conflicts }),
          tone: "error" as const,
        },
      ],
      placeholder: t("discovered.filters.conflictsPlaceholder"),
    },
  ];
}
