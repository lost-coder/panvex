// P3-FE-01: recomposed locally from UI-kit primitives.
// Phase-7 redesign: pulse row + chip-tab + status filter + search + paging.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AgeCell,
  DataTable,
  EmptyState,
  FilterBar,
  FilterChip,
  PageHeader,
  PulseRow,
  StatusLabel,
  cn,
  formatTime,
  type PulseTick,
  type PulseTone,
  type StatusTone,
} from "@/ui";
import { formatAge, shortId } from "@/shared/lib/pages-shared";
import { useNowSec } from "@/shared/hooks/useNowSec";
import { usePagination } from "@/shared/hooks/usePagination";
import type {
  ActivityPageProps,
  JobListItem,
  AuditListItem,
} from "@/shared/api/types-pages/pages";

const DAY = 86_400;

type JobTone = PulseTone;

const jobStatusVariant: Record<string, JobTone> = {
  succeeded: "ok",
  running: "warn",
  queued: "default",
  failed: "error",
  expired: "error",
};

// Backend emits snake_case action tags ("rollout_client_config",
// "agent_restart"). Pretty-case inline so the table reads like prose without
// a translation map.
function prettyAction(action: string) {
  return action.replace(/_/g, " ");
}

// ─── Jobs view ────────────────────────────────────────────────────────────────

function JobStatusCell({ status }: { status: string }) {
  const tone: StatusTone = jobStatusVariant[status] ?? "default";
  return <StatusLabel tone={tone} label={status} animate={status === "running"} />;
}

// Q4.U-Q-09: column headers come from i18n so the operator sees the
// labels in the language they picked in profile settings. Computed via
// a factory so the static import-time const can carry untranslated
// labels and the component call-site supplies the translated values.
function getJobColumns(t: (key: string) => string) {
  return [
    {
      key: "action",
      header: t("columns.action"),
      render: (j: JobListItem) => (
        <div className="flex flex-col gap-0.5 min-w-0">
          <span className="font-mono text-xs text-fg">{prettyAction(j.action)}</span>
          {j.failureReason && (
            // Failure reason as a dim second line under the action so operators
            // see *why* a job failed without opening a detail modal.
            <span
              className="text-[11px] text-status-error/80 truncate"
              title={j.failureReason}
            >
              {j.failureReason}
            </span>
          )}
        </div>
      ),
      className: "min-w-[220px] max-w-[360px]",
    },
    {
      key: "status",
      header: t("columns.status"),
      render: (j: JobListItem) => <JobStatusCell status={j.status} />,
      className: "w-[140px]",
    },
    {
      key: "targets",
      header: t("columns.targets"),
      render: (j: JobListItem) => (
        <span className="font-mono text-xs text-fg-muted tabular-nums">
          {j.targetCount === 0 ? "—" : `×${j.targetCount}`}
        </span>
      ),
      className: "hidden sm:table-cell text-center w-[80px]",
    },
    {
      key: "actor",
      header: t("columns.actor"),
      render: (j: JobListItem) => (
        <span
          className={cn(
            "text-[11px] truncate",
            j.actorLabel ? "text-fg" : "font-mono text-fg-muted",
          )}
          title={j.actorId}
        >
          {j.actorLabel ?? shortId(j.actorId)}
        </span>
      ),
      className: "hidden md:table-cell w-[140px]",
    },
    {
      key: "created",
      header: t("columns.created"),
      render: (j: JobListItem) => <AgeCell unixSec={j.createdAtUnix} mode="age" />,
      className: "text-right w-[120px]",
    },
  ];
}

// ─── Audit view ───────────────────────────────────────────────────────────────

// Audit action slugs like "user.login" / "client.update" have a namespace
// prefix — color it faintly to anchor the eye on the real verb.
function ActionCell({ action }: { action: string }) {
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

function groupByDay(events: AuditListItem[]) {
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

function AuditRowActor({ e }: { e: AuditListItem }) {
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

function AuditRowTarget({ e }: { e: AuditListItem }) {
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

function AuditList({ events }: { events: AuditListItem[] }) {
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

// ─── Main ─────────────────────────────────────────────────────────────────────

const JOB_STATUSES = ["all", "running", "queued", "succeeded", "failed"] as const;
type JobStatusFilter = (typeof JOB_STATUSES)[number];

const AUDIT_WINDOWS: { id: AuditWindow; label: string; seconds: number | null }[] = [
  { id: "1h", label: "1h", seconds: 3_600 },
  { id: "24h", label: "24h", seconds: DAY },
  { id: "7d", label: "7d", seconds: 7 * DAY },
  { id: "all", label: "All", seconds: null },
];
type AuditWindow = "1h" | "24h" | "7d" | "all";

const AUDIT_PAGE_SIZE = 50;

export function ActivityPage({
  jobs,
  auditEvents,
  activeTab,
  onTabChange,
  lookupError,
}: ActivityPageProps) {
  // Q4.U-Q-09: localised column headers.
  const { t } = useTranslation("activity");
  const jobColumns = useMemo(() => getJobColumns(t), [t]);

  const [query, setQuery] = useState("");
  const [jobStatus, setJobStatus] = useState<JobStatusFilter>("all");
  const [auditWindow, setAuditWindow] = useState<AuditWindow>("24h");

  // Auto-refreshing "now". Without this, the 24h audit window and "failed 24h"
  // counters drift as operators keep the tab open (the mount-time snapshot
  // never updated).
  const nowSec = useNowSec();
  const pulse = useMemo<PulseTick[]>(() => {
    const now = nowSec;
    const jobs24h = jobs.filter((j) => j.createdAtUnix > now - DAY);
    const running = jobs.filter((j) => j.status === "running" || j.status === "queued");
    const failed24h = jobs24h.filter(
      (j) => j.status === "failed" || j.status === "expired",
    );
    const audit24h = auditEvents.filter((e) => e.createdAtUnix > now - DAY);
    return [
      {
        label: "Jobs 24h",
        value: jobs24h.length.toLocaleString(),
        hint: `${jobs.length.toLocaleString()} total`,
      },
      {
        label: "Running now",
        value: running.length.toLocaleString(),
        hint: running.length > 0 ? "active or queued" : "nothing in flight",
        tone: running.length > 0 ? "warn" : "default",
      },
      {
        label: "Failed 24h",
        value: failed24h.length.toLocaleString(),
        hint: failed24h.length > 0 ? "needs review" : "clean window",
        tone: failed24h.length > 0 ? "error" : "default",
      },
      {
        label: "Audit 24h",
        value: audit24h.length.toLocaleString(),
        hint: `${auditEvents.length.toLocaleString()} total`,
      },
    ];
  }, [jobs, auditEvents, nowSec]);

  const filteredJobs = useMemo(() => {
    const q = query.trim().toLowerCase();
    return jobs.filter((j) => {
      if (jobStatus !== "all" && j.status !== jobStatus) return false;
      if (!q) return true;
      return (
        j.action.toLowerCase().includes(q) ||
        j.actorId.toLowerCase().includes(q) ||
        j.status.toLowerCase().includes(q)
      );
    });
  }, [jobs, query, jobStatus]);

  // Resolve the active window's cutoff once. `null` = no floor (show all).
  const auditWindowSecs = AUDIT_WINDOWS.find((w) => w.id === auditWindow)?.seconds ?? null;
  const filteredAudit = useMemo(() => {
    const q = query.trim().toLowerCase();
    const cutoff = auditWindowSecs == null ? null : nowSec - auditWindowSecs;
    return auditEvents.filter((e) => {
      if (cutoff != null && e.createdAtUnix < cutoff) return false;
      if (!q) return true;
      return (
        e.actorId.toLowerCase().includes(q) ||
        (e.actorLabel?.toLowerCase().includes(q) ?? false) ||
        e.action.toLowerCase().includes(q) ||
        e.targetId.toLowerCase().includes(q) ||
        (e.targetLabel?.toLowerCase().includes(q) ?? false)
      );
    });
  }, [auditEvents, query, auditWindowSecs, nowSec]);

  const pager = usePagination({
    pageSize: AUDIT_PAGE_SIZE,
    totalCount: filteredAudit.length,
  });
  const pagedAudit = useMemo(() => pager.slice(filteredAudit), [filteredAudit, pager]);

  const jobStatusCounts = useMemo(() => {
    const counts: Record<string, number> = { all: jobs.length };
    for (const j of jobs) counts[j.status] = (counts[j.status] ?? 0) + 1;
    return counts;
  }, [jobs]);

  return (
    <div className="flex flex-col">
      <PageHeader title="Activity" subtitle="Jobs and audit trail" />

      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        {lookupError && (
          // Non-fatal lookup warning. The list still renders below with
          // UUID fallbacks — the banner tells the operator why labels
          // look like UUIDs instead of usernames / node names.
          <div
            role="alert"
            className="rounded-xs border border-status-warn/30 bg-status-warn/10 px-3 py-2 text-xs font-mono text-status-warn"
          >
            {lookupError}
          </div>
        )}

        <PulseRow ticks={pulse} />

        {/* Tab chips + search. Chips pick the dataset; the input filters
            within it. */}
        <FilterBar
          chips={
            <>
              <FilterChip
                active={activeTab === "jobs"}
                onClick={() => onTabChange("jobs")}
                count={jobs.length}
              >
                Jobs
              </FilterChip>
              <FilterChip
                active={activeTab === "audit"}
                onClick={() => onTabChange("audit")}
                count={auditEvents.length}
              >
                Audit
              </FilterChip>
            </>
          }
          search={{
            value: query,
            onChange: setQuery,
            placeholder:
              activeTab === "jobs" ? "Search action or actor…" : "Search audit…",
          }}
        />

        {activeTab === "jobs" && (
          <div className="flex flex-wrap gap-1.5">
            {JOB_STATUSES.map((s) => (
              <FilterChip
                key={s}
                active={jobStatus === s}
                onClick={() => setJobStatus(s)}
                count={jobStatusCounts[s] ?? 0}
              >
                {s}
              </FilterChip>
            ))}
          </div>
        )}

        {/* Audit window chips — time floor for the event list. Hidden on
            jobs tab to keep the filter row scannable. */}
        {activeTab === "audit" && (
          <div className="flex flex-wrap items-center gap-3">
            <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">
              Window
            </span>
            <div className="flex gap-1.5">
              {AUDIT_WINDOWS.map((w) => (
                <FilterChip
                  key={w.id}
                  active={auditWindow === w.id}
                  onClick={() => setAuditWindow(w.id)}
                >
                  {w.label}
                </FilterChip>
              ))}
            </div>
            <span className="ml-auto text-[11px] font-mono text-fg-muted tabular-nums">
              {filteredAudit.length.toLocaleString()} event
              {filteredAudit.length === 1 ? "" : "s"}
            </span>
          </div>
        )}

        {activeTab === "jobs" ? (
          filteredJobs.length === 0 ? (
            <EmptyState
              title={jobs.length === 0 ? "Jobs will appear here" : "No jobs match the filter"}
              description={
                jobs.length === 0
                  ? "Mutations run by operators (client rollouts, runtime reloads, self-updates) are recorded here once the first one fires."
                  : "Widen the filter or clear the search."
              }
            />
          ) : (
            <DataTable data={filteredJobs} columns={jobColumns} keyExtractor={(j) => j.id} />
          )
        ) : (
          <>
            <AuditList events={pagedAudit} />
            {pager.pageCount > 1 && (
              <div className="flex items-center justify-between gap-3 px-1 py-2 text-xs">
                <span className="font-mono text-fg-muted tabular-nums">
                  Page {pager.page + 1} of {pager.pageCount}
                  <span className="ml-2 text-fg-faint">
                    · {pager.rangeStart + 1}–{pager.rangeEnd + 1} of{" "}
                    {filteredAudit.length.toLocaleString()}
                  </span>
                </span>
                <div className="flex gap-1.5">
                  <button
                    type="button"
                    onClick={pager.prev}
                    disabled={!pager.hasPrev}
                    className="rounded-xs border border-border bg-bg-card px-2.5 py-1 font-mono uppercase tracking-wider text-fg-muted transition-colors hover:text-fg disabled:opacity-40 disabled:pointer-events-none"
                  >
                    ← Prev
                  </button>
                  <button
                    type="button"
                    onClick={pager.next}
                    disabled={!pager.hasNext}
                    className="rounded-xs border border-border bg-bg-card px-2.5 py-1 font-mono uppercase tracking-wider text-fg-muted transition-colors hover:text-fg disabled:opacity-40 disabled:pointer-events-none"
                  >
                    Next →
                  </button>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
