import { useMemo } from "react";
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
            "rounded-xs px-1 py-px text-[9px] font-mono uppercase tracking-wider",
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

export function AuditList({ events }: Readonly<{ events: AuditListItem[] }>) {
  const groups = useMemo(() => groupByDay(events), [events]);
  if (events.length === 0) {
    return (
      <EmptyState
        title="Audit trail is empty"
        description="Every login, mutation, and admin action appears here. Activity from the last 30 days is retained by default."
      />
    );
  }
  return (
    <div className="flex flex-col gap-4">
      {groups.map(([day, rows]) => (
        <section key={day} className="flex flex-col rounded-xs border border-border overflow-hidden">
          <header className="bg-bg-card px-3 py-1.5 text-[10px] font-mono uppercase tracking-wider text-fg-muted border-b border-divider">
            {day}
            <span className="ml-2 text-fg-faint">({rows.length})</span>
          </header>
          <ul className="flex flex-col">
            {rows.map((e) => (
              <li
                key={e.id}
                className="flex items-center gap-3 px-3 py-2 border-b border-divider last:border-b-0 hover:bg-bg-hover transition-colors"
              >
                <span className="text-[10px] font-mono text-fg-muted tabular-nums w-[56px] shrink-0">
                  {formatTime(e.createdAtUnix)}
                </span>
                <div className="shrink-0">
                  <ActionCell action={e.action} />
                </div>
                <span className="text-[11px] text-fg-muted flex items-center gap-1.5 min-w-0 flex-1">
                  <span className="text-fg-faint shrink-0">by</span>
                  <AuditRowActor e={e} />
                  <AuditRowTarget e={e} />
                </span>
                <span className="ml-auto text-[10px] font-mono text-fg-faint shrink-0">
                  {formatAge(e.createdAtUnix)}
                </span>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
