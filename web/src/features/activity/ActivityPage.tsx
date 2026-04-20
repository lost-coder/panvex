import { PageHeader } from "@/ui/layout/PageHeader";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/ui/base/tabs";
import { DataTable } from "@/ui/components/DataTable";
import { Badge } from "@/ui/primitives/Badge";
import { Timeline } from "@/ui/compositions/Timeline";
import type { ActivityPageProps, JobListItem, AuditListItem } from "@/shared/api/types-pages/pages";
import { formatTime } from "@/shared/lib/pages-shared";

const jobStatusVariant: Record<string, "ok" | "warn" | "error" | "default"> = {
  succeeded: "ok",
  running: "warn",
  queued: "default",
  failed: "error",
  expired: "error",
};

const jobColumns = [
  {
    key: "action",
    header: "Action",
    render: (j: JobListItem) => <span className="font-mono text-xs">{j.action}</span>,
  },
  {
    key: "status",
    header: "Status",
    render: (j: JobListItem) => (
      <Badge variant={jobStatusVariant[j.status] ?? "default"}>{j.status}</Badge>
    ),
  },
  {
    key: "actor",
    header: "Actor",
    render: (j: JobListItem) => <span className="text-sm text-fg-muted">{j.actorId}</span>,
  },
  {
    key: "targets",
    header: "Targets",
    render: (j: JobListItem) => (
      <span className="text-xs text-fg-muted">
        {j.targetCount} agent{j.targetCount !== 1 ? "s" : ""}
      </span>
    ),
  },
  {
    key: "created",
    header: "Created",
    render: (j: JobListItem) => (
      <span className="text-xs text-fg-muted">{formatTime(j.createdAtUnix)}</span>
    ),
  },
];

function auditToTimeline(events: AuditListItem[]) {
  return events.map((e) => ({
    status: "ok" as const,
    time: formatTime(e.createdAtUnix),
    message: `${e.actorId} — ${e.action}`,
    detail: e.targetId,
  }));
}

export function ActivityPage({ jobs, auditEvents, activeTab, onTabChange }: ActivityPageProps) {
  return (
    <div className="flex flex-col">
      <PageHeader title="Activity" subtitle="Jobs and audit trail" />

      <div className="px-4 md:px-8 pb-8">
        <Tabs value={activeTab} onValueChange={onTabChange}>
          <TabsList>
            <TabsTrigger value="jobs">Jobs ({jobs.length})</TabsTrigger>
            <TabsTrigger value="audit">Audit ({auditEvents.length})</TabsTrigger>
          </TabsList>

          <TabsContent value="jobs" className="mt-4">
            {jobs.length === 0 ? (
              <div className="text-center text-sm text-fg-muted py-8">No jobs recorded yet.</div>
            ) : (
              <DataTable data={jobs} columns={jobColumns} keyExtractor={(j) => j.id} />
            )}
          </TabsContent>

          <TabsContent value="audit" className="mt-4">
            {auditEvents.length === 0 ? (
              <div className="text-center text-sm text-fg-muted py-8">No audit events yet.</div>
            ) : (
              <Timeline events={auditToTimeline(auditEvents)} />
            )}
          </TabsContent>
        </Tabs>
      </div>
    </div>
  );
}
