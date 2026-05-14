// R-Q-08: filter spec builder + bulk-actions list extracted from
// ClientsPage.tsx. Pure factory functions — no hooks — so the host
// page just plugs the result into TableView / BulkActionBar.

import type { TFunction } from "i18next";

import type { BulkClientAction } from "@/ui";

import type { ClientCounts } from "./ClientsPagePulse";

export interface ClientsStatusFilterOptions {
  value: string;
  onChange: (next: string) => void;
  counts: ClientCounts;
  t: TFunction<"clients">;
}

export function buildClientsStatusFilter(opts: ClientsStatusFilterOptions) {
  const { t } = opts;
  return {
    key: "status",
    value: opts.value,
    onChange: opts.onChange,
    // Inline chip toggle so the four statuses are one click away —
    // no dropdown step for the most-used filter on a multi-thousand
    // client list.
    variant: "chips" as const,
    options: [
      { value: "all", label: t("filters.status.all", { count: opts.counts.all }) },
      {
        value: "active",
        label: t("filters.status.active", { count: opts.counts.active }),
        tone: "ok" as const,
      },
      {
        value: "disabled",
        label: t("filters.status.disabled", { count: opts.counts.disabled }),
        tone: "warn" as const,
      },
      {
        value: "expired",
        label: t("filters.status.expired", { count: opts.counts.expired }),
        tone: "error" as const,
      },
    ],
    placeholder: t("filters.status.placeholder"),
  };
}

export function buildClientsBulkActions(
  enabled: boolean,
  t: TFunction<"clients">,
): Array<{
  id: BulkClientAction;
  label: string;
  variant: "ghost";
  disabled: boolean;
}> {
  return [
    { id: "enable", label: t("filters.bulk.enable"), variant: "ghost", disabled: !enabled },
    { id: "disable", label: t("filters.bulk.disable"), variant: "ghost", disabled: !enabled },
    { id: "delete", label: t("filters.bulk.delete"), variant: "ghost", disabled: !enabled },
  ];
}
