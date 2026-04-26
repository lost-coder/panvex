// R-Q-08: 4-tile pulse strip for /clients/discovered, extracted from
// DiscoveredClientsPage.tsx. Pure presentation, takes precomputed
// counts.
//
// R-Q-24: pulse component + filter-spec factory co-located by design.
/* eslint-disable react-refresh/only-export-components */

import { DiscoveredPulseCell } from "./DiscoveredMobileRow";

export interface DiscoveredCounts {
  all: number;
  pending: number;
  adopted: number;
  ignored: number;
  conflicts: number;
}

export function DiscoveredPulseStrip({ counts }: { counts: DiscoveredCounts }) {
  return (
    <section className="rounded-xs bg-bg-card border border-border grid grid-cols-2 md:grid-cols-4">
      <DiscoveredPulseCell i={0} label="Pending" value={counts.pending} tone="warn" />
      <DiscoveredPulseCell i={1} label="Adopted" value={counts.adopted} tone="ok" />
      <DiscoveredPulseCell i={2} label="Ignored" value={counts.ignored} tone="default" />
      <DiscoveredPulseCell
        i={3}
        label="Conflicts"
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
}

export function buildDiscoveredFilters(opts: DiscoveredFilterOptions) {
  return [
    {
      key: "status",
      value: opts.status.value,
      onChange: opts.status.onChange,
      variant: "chips" as const,
      options: [
        { value: "all", label: `All · ${opts.counts.all}` },
        { value: "pending", label: `Pending · ${opts.counts.pending}`, tone: "warn" as const },
        { value: "adopted", label: `Adopted · ${opts.counts.adopted}`, tone: "ok" as const },
        { value: "ignored", label: `Ignored · ${opts.counts.ignored}` },
      ],
      placeholder: "Status",
    },
    {
      key: "conflicts",
      value: opts.conflicts.value,
      onChange: opts.conflicts.onChange,
      variant: "chips" as const,
      options: [
        { value: "all", label: "All" },
        {
          value: "only",
          label: `Conflicts · ${opts.counts.conflicts}`,
          tone: "error" as const,
        },
      ],
      placeholder: "Conflicts",
    },
  ];
}
