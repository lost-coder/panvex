import type { TFunction } from "i18next";
import { memo } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/ui";
import type { ConnectionClassCount } from "@/shared/api/types-pages/pages";

/**
 * Bad-connection breakdown card. Renders the two Telemt-3.4.10 class
 * histograms (`connections_bad_by_class`, `handshake_failures_by_class`)
 * as two stacked sections inside one card.
 *
 * Visible in all transport modes — ME and Direct nodes alike accumulate
 * these counters because the classifier runs on every inbound TCP
 * handshake regardless of where the connection routes afterwards.
 *
 * The class string set is open: Telemt may add labels in future
 * versions without an agent or panel upgrade. The component does not
 * enforce a known-class allow-list — unrecognised classes render with
 * a humanised fallback (snake_case → "Snake case").
 */
export const BadConnectionsCard = memo(BadConnectionsCardInner);

interface BadConnectionsCardProps {
  connectionsBadByClass: ConnectionClassCount[];
  handshakeFailuresByClass: ConnectionClassCount[];
}

function BadConnectionsCardInner({
  connectionsBadByClass,
  handshakeFailuresByClass,
}: Readonly<BadConnectionsCardProps>) {
  const { t } = useTranslation("servers");
  const sortedBad = sortByTotalDesc(connectionsBadByClass);
  const sortedHandshake = sortByTotalDesc(handshakeFailuresByClass);
  const bothEmpty = sortedBad.length === 0 && sortedHandshake.length === 0;

  return (
    <section className="rounded-xs bg-bg-card border border-border p-4 flex flex-col gap-4">
      <header className="flex items-center justify-between pb-2 border-b border-divider">
        <span className="text-sm font-semibold text-fg">
          {t("detail.badConnections.title")}
        </span>
      </header>
      {bothEmpty ? (
        <p className="text-xs text-fg-muted">
          {t("detail.badConnections.emptyBoth")}
        </p>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-4">
          <ClassBreakdownColumn
            heading={t("detail.badConnections.sectionBadHeader")}
            rows={sortedBad}
            emptyLabel={t("detail.badConnections.emptyBad")}
          />
          <ClassBreakdownColumn
            heading={t("detail.badConnections.sectionHandshakeHeader")}
            rows={sortedHandshake}
            emptyLabel={t("detail.badConnections.emptyHandshake")}
          />
        </div>
      )}
    </section>
  );
}

function sortByTotalDesc(rows: ConnectionClassCount[]): ConnectionClassCount[] {
  return [...rows].sort((a, b) => b.total - a.total || a.class.localeCompare(b.class));
}

function ClassBreakdownColumn({
  heading,
  rows,
  emptyLabel,
}: Readonly<{
  heading: string;
  rows: ConnectionClassCount[];
  emptyLabel: string;
}>) {
  const { t } = useTranslation("servers");
  const total = rows.reduce((s, r) => s + r.total, 0);

  return (
    <div className="flex flex-col gap-2 min-w-0">
      <div className="flex items-center justify-between">
        <span className="text-nano font-mono uppercase tracking-wider text-fg-muted">
          {heading}
        </span>
        <span className="text-nano font-mono text-fg-muted">
          {t("detail.badConnections.totalLabel")}: {total.toLocaleString()}
        </span>
      </div>
      {rows.length === 0 ? (
        <span className="text-xs text-fg-muted py-2">{emptyLabel}</span>
      ) : (
        <ul className="flex flex-col">
          {rows.map((row, idx) => (
            <ClassRow
              key={row.class}
              row={row}
              total={total}
              dim={idx > 0 && row.total === 0}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function ClassRow({
  row,
  total,
  dim,
}: Readonly<{
  row: ConnectionClassCount;
  total: number;
  dim: boolean;
}>) {
  const { t } = useTranslation("servers");
  const pct = total > 0 ? Math.round((row.total / total) * 100) : 0;
  const label = humaniseClass(row.class, t);
  const tone = severityForClass(row.class);
  return (
    <li
      className={cn(
        "flex items-center justify-between gap-3 py-2 border-b border-dashed border-divider last:border-b-0",
        dim && "opacity-60",
      )}
    >
      <div className="flex items-center gap-2 min-w-0">
        <span
          className={cn(
            "h-1.5 w-1.5 rounded-full shrink-0",
            tone === "error" && "bg-status-error",
            tone === "warn" && "bg-status-warn",
            tone === "neutral" && "bg-fg-muted/60",
          )}
        />
        <span className="text-xs text-fg truncate" title={row.class}>
          {label}
        </span>
      </div>
      <div className="flex items-center gap-3 shrink-0">
        <span className="text-nano font-mono text-fg-muted tabular-nums">
          {t("detail.badConnections.ratioLabel", { pct })}
        </span>
        <span className="text-xs font-mono font-semibold text-fg tabular-nums">
          {row.total.toLocaleString()}
        </span>
      </div>
    </li>
  );
}

function severityForClass(cls: string): "error" | "warn" | "neutral" {
  if (cls === "unknown_tls_sni") return "warn";
  if (cls.startsWith("expected_64_got_0_")) return "neutral";
  return "neutral";
}

function humaniseClass(cls: string, t: TFunction<"servers">): string {
  const key = `detail.badConnections.classLabels.${cls}`;
  const translated = t(key);
  if (translated !== key) return translated;
  // Fallback: snake_case → "Snake case"
  return cls
    .replace(/_/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}
