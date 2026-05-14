// R-Q-08: pending + reviewed list sections extracted from
// DiscoveredClientsPage.tsx. The host page hands precomputed groups,
// columns, and selection state; this file owns only the responsive
// switch between MobileRow and DataTable.

import { useTranslation } from "react-i18next";

import { Button, DataTable } from "@/ui";
import type { DiscoveredGroup } from "@/features/clients/lib/groupDiscovered";

import { DiscoveredMobileRow } from "./DiscoveredMobileRow";

export interface DiscoveredPendingSectionProps {
  rows: DiscoveredGroup[];
  columns: ReturnType<
    // import-cycle dodge — the column factory's row type is the
    // same DiscoveredGroup we already accept above.
    () => Parameters<typeof DataTable<DiscoveredGroup>>[0]["columns"]
  >;
  selected: Set<string>;
  selectedRecordCount: number;
  onToggleSelect: (key: string) => void;
  onAdopt: (ids: string[]) => void;
  onIgnore: (ids: string[]) => void;
  onClearSelection: () => void;
  onBulkAdopt: () => void;
  onBulkIgnore: () => void;
  busy?: boolean | undefined;
}

export function DiscoveredPendingSection(props: Readonly<DiscoveredPendingSectionProps>) {
  const { t } = useTranslation("clients");
  const {
    rows,
    columns,
    selected,
    selectedRecordCount,
    onToggleSelect,
    onAdopt,
    onIgnore,
    onClearSelection,
    onBulkAdopt,
    onBulkIgnore,
    busy,
  } = props;
  if (rows.length === 0) return null;
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h3 className="text-sm font-semibold text-fg">
          {t("discovered.pendingSection", { count: rows.length })}
        </h3>
        {selected.size > 0 && (
          <div className="flex items-center gap-2 rounded-xs bg-bg-card border border-accent/40 px-3 py-1.5">
            <span className="text-xs font-mono text-fg">
              {t("discovered.selection.summary", {
                count: selected.size,
                records: selectedRecordCount,
              })}
            </span>
            <Button size="sm" disabled={busy} onClick={onBulkAdopt}>
              {t("discovered.selection.adopt")}
            </Button>
            <Button size="sm" variant="outline" disabled={busy} onClick={onBulkIgnore}>
              {t("discovered.selection.ignore")}
            </Button>
            <Button size="sm" variant="ghost" onClick={onClearSelection}>
              {t("discovered.selection.clear")}
            </Button>
          </div>
        )}
      </div>
      <div className="md:hidden rounded-xs bg-bg-card border border-border overflow-hidden">
        {rows.map((g) => (
          <DiscoveredMobileRow
            key={g.key}
            row={g}
            selected={selected.has(g.key)}
            onToggleSelect={onToggleSelect}
            onAdopt={onAdopt}
            onIgnore={onIgnore}
            busy={busy}
          />
        ))}
      </div>
      <div className="hidden md:block rounded-xs bg-bg-card border border-border overflow-hidden">
        <DataTable
          columns={columns}
          data={rows}
          keyExtractor={(row: Readonly<DiscoveredGroup>) => row.key}
        />
      </div>
    </section>
  );
}

export interface DiscoveredReviewedSectionProps {
  rows: DiscoveredGroup[];
  columns: Parameters<typeof DataTable<DiscoveredGroup>>[0]["columns"];
  busy?: boolean | undefined;
}

export function DiscoveredReviewedSection({
  rows,
  columns,
  busy,
}: Readonly<DiscoveredReviewedSectionProps>) {
  const { t } = useTranslation("clients");
  if (rows.length === 0) return null;
  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-sm font-semibold text-fg-muted">
        {t("discovered.reviewedSection", { count: rows.length })}
      </h3>
      <div className="md:hidden rounded-xs bg-bg-card border border-border overflow-hidden">
        {rows.map((g) => (
          <DiscoveredMobileRow
            key={g.key}
            row={g}
            selected={false}
            onToggleSelect={() => {}}
            busy={busy}
          />
        ))}
      </div>
      <div className="hidden md:block rounded-xs bg-bg-card border border-border overflow-hidden">
        <DataTable
          columns={columns}
          data={rows}
          keyExtractor={(row: Readonly<DiscoveredGroup>) => row.key}
        />
      </div>
    </section>
  );
}
