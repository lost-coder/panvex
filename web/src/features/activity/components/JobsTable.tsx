import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  AgeCell,
  DataTable,
  StatusLabel,
  cn,
  type PulseTone,
  type StatusTone,
} from "@/ui";
import { shortId } from "@/shared/lib/pages-shared";
import type { JobListItem } from "@/shared/api/types-pages/pages";

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
export function prettyAction(action: string) {
  return action.replaceAll("_", " ");
}

export function JobStatusCell({ status }: Readonly<{ status: string }>) {
  const tone: StatusTone = jobStatusVariant[status] ?? "default";
  return <StatusLabel tone={tone} label={status} animate={status === "running"} />;
}

// Q4.U-Q-09: column headers come from i18n so the operator sees the
// labels in the language they picked in profile settings. Computed via
// a factory so the static import-time const can carry untranslated
// labels and the component call-site supplies the translated values.
export function getJobColumns(t: (key: string) => string) {
  return [
    {
      key: "action",
      header: t("columns.action"),
      render: (j: Readonly<JobListItem>) => (
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
      render: (j: Readonly<JobListItem>) => <JobStatusCell status={j.status} />,
      className: "w-[140px]",
    },
    {
      key: "targets",
      header: t("columns.targets"),
      render: (j: Readonly<JobListItem>) => (
        <span className="font-mono text-xs text-fg-muted tabular-nums">
          {j.targetCount === 0 ? "—" : `×${j.targetCount}`}
        </span>
      ),
      className: "hidden sm:table-cell text-center w-[80px]",
    },
    {
      key: "actor",
      header: t("columns.actor"),
      render: (j: Readonly<JobListItem>) => (
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
      render: (j: Readonly<JobListItem>) => <AgeCell unixSec={j.createdAtUnix} mode="age" />,
      className: "text-right w-[120px]",
    },
  ];
}

export function JobsTable({ jobs }: Readonly<{ jobs: JobListItem[] }>) {
  const { t } = useTranslation("activity");
  const columns = useMemo(() => getJobColumns(t), [t]);
  return (
    <DataTable data={jobs} columns={columns} keyExtractor={(j) => j.id} />
  );
}
