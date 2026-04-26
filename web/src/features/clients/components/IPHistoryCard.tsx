// Q5.U-Q-08: extracted from ClientDetailPage.tsx so the page-level
// orchestrator stays under ~600 LOC. The card has no dependency on
// the page's local state, so a clean component move was sufficient.
import { DataTable, MonoValue } from "@/ui";

export interface IPRow {
  ip: string;
  firstSeen: string;
  lastSeen: string;
  countryCode?: string | undefined;
  countryName?: string | undefined;
  city?: string | undefined;
  asn?: string | undefined;
}

export function IPHistoryCard({
  ips,
  totalUnique,
}: {
  ips: IPRow[];
  totalUnique: number;
}) {
  const columns = [
    {
      key: "ip",
      header: "IP",
      render: (row: IPRow) => <MonoValue>{row.ip}</MonoValue>,
      className: "w-[160px]",
    },
    {
      key: "country",
      header: "Country",
      render: (row: IPRow) =>
        row.countryName || row.countryCode ? (
          <span className="text-xs text-fg">
            {row.countryCode && (
              <span className="font-mono text-[10px] text-fg-muted mr-1">{row.countryCode}</span>
            )}
            {row.countryName ?? ""}
          </span>
        ) : (
          <span className="text-xs text-fg-faint">—</span>
        ),
      className: "hidden md:table-cell w-[160px]",
    },
    {
      key: "city",
      header: "City",
      render: (row: IPRow) =>
        row.city ? (
          <span className="text-xs text-fg">{row.city}</span>
        ) : (
          <span className="text-xs text-fg-faint">—</span>
        ),
      className: "hidden lg:table-cell w-[140px]",
    },
    {
      key: "asn",
      header: "ASN",
      render: (row: IPRow) =>
        row.asn ? (
          <MonoValue className="text-xs">{row.asn}</MonoValue>
        ) : (
          <span className="text-xs text-fg-faint">—</span>
        ),
      className: "hidden xl:table-cell w-[120px]",
    },
    {
      key: "firstSeen",
      header: "First seen",
      render: (row: IPRow) => (
        <span className="text-[11px] font-mono text-fg-muted tabular-nums">
          {new Date(row.firstSeen).toLocaleString()}
        </span>
      ),
      className: "hidden md:table-cell w-[170px]",
    },
    {
      key: "lastSeen",
      header: "Last seen",
      render: (row: IPRow) => (
        <span className="text-[11px] font-mono text-fg tabular-nums">
          {new Date(row.lastSeen).toLocaleString()}
        </span>
      ),
      className: "w-[170px]",
    },
  ];
  return (
    <section className="rounded-xs bg-bg-card border border-divider overflow-hidden">
      <header className="px-4 py-3 border-b border-divider flex items-center justify-between gap-2">
        <div className="flex items-baseline gap-2">
          <span className="text-sm font-semibold text-fg">IP history</span>
          <span className="text-[11px] font-mono text-fg-muted">{totalUnique} unique</span>
        </div>
        <span className="text-[10px] font-mono text-fg-muted truncate">
          GeoIP enrichment pending — see backend-followup #3
        </span>
      </header>
      {ips.length === 0 ? (
        <div className="px-4 py-8 text-sm text-fg-muted text-center">
          No IP activity recorded.
        </div>
      ) : (
        <DataTable
          columns={columns}
          data={ips}
          keyExtractor={(row) => row.ip}
          emptyMessage="No IPs"
        />
      )}
    </section>
  );
}
