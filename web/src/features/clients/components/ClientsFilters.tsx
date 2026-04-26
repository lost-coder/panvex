// R-Q-08: filter spec builder + bulk-actions list extracted from
// ClientsPage.tsx. Pure factory functions — no hooks — so the host
// page just plugs the result into TableView / BulkActionBar.

import type { BulkClientAction } from "@/ui";

import type { ClientCounts } from "./ClientsPagePulse";

export interface ClientsStatusFilterOptions {
  value: string;
  onChange: (next: string) => void;
  counts: ClientCounts;
}

export function buildClientsStatusFilter(opts: ClientsStatusFilterOptions) {
  return {
    key: "status",
    value: opts.value,
    onChange: opts.onChange,
    // Inline chip toggle so the four statuses are one click away —
    // no dropdown step for the most-used filter on a multi-thousand
    // client list.
    variant: "chips" as const,
    options: [
      { value: "all", label: `All · ${opts.counts.all}` },
      { value: "active", label: `Active · ${opts.counts.active}`, tone: "ok" as const },
      { value: "disabled", label: `Disabled · ${opts.counts.disabled}`, tone: "warn" as const },
      { value: "expired", label: `Expired · ${opts.counts.expired}`, tone: "error" as const },
    ],
    placeholder: "Status",
  };
}

export function buildClientsBulkActions(enabled: boolean): Array<{
  id: BulkClientAction;
  label: string;
  variant: "ghost";
  disabled: boolean;
}> {
  return [
    { id: "enable", label: "Enable", variant: "ghost", disabled: !enabled },
    { id: "disable", label: "Disable", variant: "ghost", disabled: !enabled },
    { id: "delete", label: "Delete", variant: "ghost", disabled: !enabled },
  ];
}
