import { useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  EmptyState,
  cn,
  formatTime,
} from "@/ui";
import { formatAge, shortId } from "@/shared/lib/pages-shared";
import type { AuditListItem } from "@/shared/api/types-pages/pages";

// Audit action slugs like "user.login" / "client.update" have a namespace
// prefix — color it faintly to anchor the eye on the real verb.
export function ActionCell({ action }: Readonly<{ action: string }>) {
  const dot = action.indexOf(".");
  if (dot < 0) {
    return <span className="font-mono text-xs text-fg">{action}</span>;
  }
  return (
    <span className="font-mono text-xs">
      <span className="text-fg-muted">{action.slice(0, dot + 1)}</span>
      <span className="text-fg">{action.slice(dot + 1)}</span>
    </span>
  );
}

export function groupByDay(events: AuditListItem[]) {
  const groups = new Map<string, AuditListItem[]>();
  for (const e of events) {
    const d = new Date(e.createdAtUnix * 1000);
    const key = d.toLocaleDateString(undefined, {
      year: "numeric",
      month: "short",
      day: "numeric",
    });
    const bucket = groups.get(key) ?? [];
    bucket.push(e);
    groups.set(key, bucket);
  }
  return Array.from(groups.entries());
}

export function AuditRowActor({ e }: Readonly<{ e: AuditListItem }>) {
  const label = e.actorLabel ?? shortId(e.actorId);
  return (
    <span
      className={cn(
        "truncate",
        e.actorLabel ? "text-fg" : "font-mono text-fg-muted",
      )}
      title={e.actorId}
    >
      {label}
    </span>
  );
}

export function AuditRowTarget({ e }: Readonly<{ e: AuditListItem }>) {
  if (!e.targetId) return null;
  const label = e.targetLabel ?? shortId(e.targetId);
  const kindTone: Record<string, string> = {
    user: "bg-accent/10 text-accent",
    client: "bg-status-ok/10 text-status-ok",
    agent: "bg-status-warn/15 text-status-warn",
  };
  return (
    <span className="inline-flex items-center gap-1.5 truncate">
      <span className="text-fg-faint">→</span>
      {e.targetKind && (
        <span
          className={cn(
            "rounded-xs px-1 py-px text-pico font-mono uppercase tracking-wider",
            kindTone[e.targetKind] ?? "bg-fg-faint/30 text-fg-muted",
          )}
        >
          {e.targetKind}
        </span>
      )}
      <span
        className={cn(
          "truncate",
          e.targetLabel ? "text-fg" : "font-mono text-fg-muted",
        )}
        title={e.targetId}
      >
        {label}
      </span>
    </span>
  );
}

// Flat-item shape consumed by the virtualizer. We flatten the per-day
// groups into a single linear list of `header` and `row` items so a
// single useVirtualizer can drive the scroll surface — driving multiple
// nested scrollers (one per day) would defeat the purpose since the
// outermost scrollbar is what the user actually scrolls.
type HeaderItem = { kind: "header"; day: string; count: number };
type RowItem = { kind: "row"; e: AuditListItem };
type Item = HeaderItem | RowItem;

function flattenItems(events: AuditListItem[]): Item[] {
  const groups = groupByDay(events);
  const out: Item[] = [];
  for (const [day, rows] of groups) {
    out.push({ kind: "header", day, count: rows.length });
    for (const e of rows) {
      out.push({ kind: "row", e });
    }
  }
  return out;
}

// Estimated heights match the actual rendered density (px-3 py-1.5 header,
// px-3 py-2 row). measureElement refines these once each item mounts so
// scroll-position math stays accurate even if a row wraps — the estimate
// only matters until the row first enters the viewport.
const HEADER_ESTIMATE = 28;
const ROW_ESTIMATE = 36;

// P-8 (Plan 4 / BP-Audit): virtualize the audit feed. With 5000 events
// the previous render emitted 5000+ DOM nodes; this version keeps only
// the slice intersecting the viewport (plus overscan) on the page.
export function AuditList({ events }: Readonly<{ events: AuditListItem[] }>) {
  const { t } = useTranslation("activity");
  const items = useMemo(() => flattenItems(events), [events]);
  const parentRef = useRef<HTMLDivElement | null>(null);

  // R-Q-24: TanStack Virtual returns memoization-incompatible callbacks;
  // mirror the lint-suppression used in DataTable.tsx for consistency.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => {
      const item = items[index];
      return item?.kind === "header" ? HEADER_ESTIMATE : ROW_ESTIMATE;
    },
    overscan: 8,
  });

  if (events.length === 0) {
    return (
      <EmptyState
        title={t("empty.auditTitle")}
        description={t("empty.auditDescription")}
      />
    );
  }

  const virtualItems = virtualizer.getVirtualItems();

  return (
    <div
      ref={parentRef}
      // The parent must be the scroll container (overflow:auto) and have
      // a bounded height — otherwise the virtualizer cannot determine
      // which items are visible. 70vh keeps the feed tall on desktop
      // while leaving room for the surrounding ActivityPage chrome.
      className="rounded-xs border border-border overflow-auto bg-bg-card"
      style={{ maxHeight: "70vh" }}
      role="list"
      aria-label={t("auditList.label")}
    >
      <div
        // Inner spacer gives the scrollbar the correct total range.
        // Children are positioned absolutely against this spacer.
        style={{ height: virtualizer.getTotalSize(), width: "100%", position: "relative" }}
      >
        {virtualItems.map((virtualRow) => {
          const item = items[virtualRow.index];
          if (!item) return null;
          const transform = `translateY(${virtualRow.start}px)`;
          if (item.kind === "header") {
            return (
              <div
                key={`h:${item.day}`}
                ref={virtualizer.measureElement}
                data-index={virtualRow.index}
                className="absolute left-0 top-0 w-full bg-bg-card px-3 py-1.5 text-nano font-mono uppercase tracking-wider text-fg-muted border-b border-divider"
                style={{ transform }}
              >
                {item.day}
                <span className="ml-2 text-fg-faint">({item.count})</span>
              </div>
            );
          }
          const e = item.e;
          return (
            <div
              key={e.id}
              ref={virtualizer.measureElement}
              data-index={virtualRow.index}
              role="listitem"
              className="absolute left-0 top-0 w-full flex items-center gap-3 px-3 py-2 border-b border-divider hover:bg-bg-hover transition-colors"
              style={{ transform }}
            >
              <span className="text-nano font-mono text-fg-muted tabular-nums w-[56px] shrink-0">
                {formatTime(e.createdAtUnix)}
              </span>
              <div className="shrink-0">
                <ActionCell action={e.action} />
              </div>
              <span className="text-micro text-fg-muted flex items-center gap-1.5 min-w-0 flex-1">
                <span className="text-fg-faint shrink-0">{t("auditList.by")}</span>
                <AuditRowActor e={e} />
                <AuditRowTarget e={e} />
              </span>
              <span className="ml-auto text-nano font-mono text-fg-faint shrink-0">
                {formatAge(e.createdAtUnix)}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
