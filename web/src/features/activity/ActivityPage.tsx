// P3-FE-01: recomposed locally from UI-kit primitives.
// Phase-7 redesign: pulse row + chip-tab + status filter + search + paging.
import { useMemo, useState } from "react";
import {
  DataTable,
  EmptyState,
  FilterBar,
  FilterChip,
  PageHeader,
  PulseRow,
  type PulseTick,
  type PulseTone,
} from "@/ui";
import { useNowSec } from "@/shared/hooks/useNowSec";
import { usePagination } from "@/shared/hooks/usePagination";
import type {
  ActivityPageProps,
} from "@/shared/api/types-pages/pages";
import { AuditList } from "./components/AuditList";
import { getJobColumns } from "./components/JobsTable";
import { useTranslation } from "react-i18next";

const DAY = 86_400;

type JobTone = PulseTone;

const JOB_STATUSES = ["all", "running", "queued", "succeeded", "partial", "failed"] as const;
type JobStatusFilter = (typeof JOB_STATUSES)[number];

type AuditWindow = "1h" | "24h" | "7d" | "all";
const AUDIT_WINDOWS: { id: AuditWindow; seconds: number | null }[] = [
  { id: "1h", seconds: 3_600 },
  { id: "24h", seconds: DAY },
  { id: "7d", seconds: 7 * DAY },
  { id: "all", seconds: null },
];

const AUDIT_PAGE_SIZE = 50;

export function ActivityPage({
  jobs,
  auditEvents,
  activeTab,
  onTabChange,
  lookupError,
}: Readonly<ActivityPageProps>) {
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
        label: t("pulse.jobs24h"),
        value: jobs24h.length.toLocaleString(),
        hint: t("pulse.jobsTotal", { count: jobs.length }),
      },
      {
        label: t("pulse.runningNow"),
        value: running.length.toLocaleString(),
        hint: running.length > 0 ? t("pulse.runningHint") : t("pulse.runningIdleHint"),
        tone: running.length > 0 ? ("warn" as JobTone) : ("default" as JobTone),
      },
      {
        label: t("pulse.failed24h"),
        value: failed24h.length.toLocaleString(),
        hint: failed24h.length > 0 ? t("pulse.failedHint") : t("pulse.failedCleanHint"),
        tone: failed24h.length > 0 ? ("error" as JobTone) : ("default" as JobTone),
      },
      {
        label: t("pulse.audit24h"),
        value: audit24h.length.toLocaleString(),
        hint: t("pulse.auditTotal", { count: auditEvents.length }),
      },
    ];
  }, [jobs, auditEvents, nowSec, t]);

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
      <PageHeader title={t("page.title")} subtitle={t("page.subtitle")} />

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
                {t("tabs.jobs")}
              </FilterChip>
              <FilterChip
                active={activeTab === "audit"}
                onClick={() => onTabChange("audit")}
                count={auditEvents.length}
              >
                {t("tabs.audit")}
              </FilterChip>
            </>
          }
          search={{
            value: query,
            onChange: setQuery,
            placeholder:
              activeTab === "jobs"
                ? t("search.jobsPlaceholder")
                : t("search.auditPlaceholder"),
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
                {t(`jobStatus.${s}`)}
              </FilterChip>
            ))}
          </div>
        )}

        {/* Audit window chips — time floor for the event list. Hidden on
            jobs tab to keep the filter row scannable. */}
        {activeTab === "audit" && (
          <div className="flex flex-wrap items-center gap-3">
            <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">
              {t("auditWindow.label")}
            </span>
            <div className="flex gap-1.5">
              {AUDIT_WINDOWS.map((w) => (
                <FilterChip
                  key={w.id}
                  active={auditWindow === w.id}
                  onClick={() => setAuditWindow(w.id)}
                >
                  {t(`auditWindow.${w.id}`)}
                </FilterChip>
              ))}
            </div>
            <span className="ml-auto text-[11px] font-mono text-fg-muted tabular-nums">
              {t("auditWindow.events", { count: filteredAudit.length })}
            </span>
          </div>
        )}

        {activeTab === "jobs" && filteredJobs.length === 0 && (
          <EmptyState
            title={jobs.length === 0 ? t("empty.jobsTitle") : t("empty.jobsFilteredTitle")}
            description={
              jobs.length === 0
                ? t("empty.jobsDescription")
                : t("empty.jobsFilteredDescription")
            }
          />
        )}
        {activeTab === "jobs" && filteredJobs.length > 0 && (
          <DataTable data={filteredJobs} columns={jobColumns} keyExtractor={(j) => j.id} />
        )}
        {activeTab !== "jobs" && (
          <>
            <AuditList events={pagedAudit} />
            {pager.pageCount > 1 && (
              <div className="flex items-center justify-between gap-3 px-1 py-2 text-xs">
                <span className="font-mono text-fg-muted tabular-nums">
                  {t("pager.page", { page: pager.page + 1, total: pager.pageCount })}
                  <span className="ml-2 text-fg-faint">
                    {t("pager.range", {
                      start: pager.rangeStart + 1,
                      end: pager.rangeEnd + 1,
                      total: filteredAudit.length.toLocaleString(),
                    })}
                  </span>
                </span>
                <div className="flex gap-1.5">
                  <button
                    type="button"
                    onClick={pager.prev}
                    disabled={!pager.hasPrev}
                    className="rounded-xs border border-border bg-bg-card px-2.5 py-1 font-mono uppercase tracking-wider text-fg-muted transition-colors hover:text-fg disabled:opacity-40 disabled:pointer-events-none"
                  >
                    {t("pager.prev")}
                  </button>
                  <button
                    type="button"
                    onClick={pager.next}
                    disabled={!pager.hasNext}
                    className="rounded-xs border border-border bg-bg-card px-2.5 py-1 font-mono uppercase tracking-wider text-fg-muted transition-colors hover:text-fg disabled:pointer-events-none disabled:opacity-40"
                  >
                    {t("pager.next")}
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
