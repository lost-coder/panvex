// P3-FE-01: recomposed locally from UI-kit primitives/components/compositions
// instead of importing the pre-built page from @lost-coder/panvex-ui/pages.
import { useState } from "react";
import {
  AlertStrip,
  Badge,
  Button,
  ClientFormSheet,
  DiscoveredClientsBanner,
  NodeCard,
  NodeSummaryCard,
  PageHeader,
  SectionHeader,
  Sheet,
  SheetBody,
  SheetContent,
  SwipeTabView,
  Timeline,
  type ClientFormData,
  type DashboardOverviewData,
  type DashboardPageProps,
  type DashboardTimelineData,
} from "@lost-coder/panvex-ui";

interface OverviewPanelProps {
  data: DashboardOverviewData;
  onNodeClick?: (nodeId: string) => void;
}

function KpiStrip({ kpis }: { kpis: DashboardOverviewData["kpis"] }) {
  return (
    <>
      {/* Mobile: compact text */}
      <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-xs font-mono md:hidden">
        {kpis.map((k) => (
          <span key={k.label} className="text-fg-muted">
            {k.label.toLowerCase()}{" "}
            <span className={k.accent ? "text-status-ok font-medium" : "text-fg font-medium"}>
              {k.value}
            </span>
          </span>
        ))}
      </div>
      {/* Desktop: card grid */}
      <div className="hidden md:grid grid-cols-4 gap-3">
        {kpis.map((k) => (
          <div
            key={k.label}
            className="rounded-xs bg-bg-card border border-border px-4 py-3 flex flex-col gap-0.5"
          >
            <span className="text-[10px] text-fg-muted uppercase tracking-wider">{k.label}</span>
            <span
              className={`text-xl font-mono font-semibold leading-tight ${k.accent ? "text-status-ok" : "text-fg"}`}
            >
              {k.value}
            </span>
            {k.sub && <span className="text-[10px] text-fg-muted">{k.sub}</span>}
          </div>
        ))}
      </div>
    </>
  );
}

function OverviewPanel({ data, onNodeClick }: OverviewPanelProps) {
  return (
    <div className="flex flex-col gap-5">
      <KpiStrip kpis={data.kpis} />

      {data.alerts.length > 0 && (
        <div>
          <SectionHeader title="Active Alerts" badge={data.alerts.length} />
          <AlertStrip alerts={data.alerts} />
        </div>
      )}

      {data.attentionNodes.length > 0 && (
        <div>
          <SectionHeader title="Attention Required" badge={data.attentionNodes.length} />
          <div className="grid grid-cols-1 2xl:grid-cols-2 gap-3">
            {data.attentionNodes.map((node) => (
              <NodeSummaryCard
                key={node.id}
                name={node.name}
                status={node.status === "error" ? "error" : "warn"}
                connections={node.connections}
                trafficBytes={node.trafficBytes}
                cpuPct={node.cpuPct}
                memPct={node.memPct}
                dcs={node.dcs}
                onClick={() => onNodeClick?.(node.id)}
              />
            ))}
          </div>
        </div>
      )}

      <div>
        <SectionHeader title="Active Servers" badge={data.healthyNodes.length} />
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {data.healthyNodes.map((node) => (
            <NodeCard
              key={node.id}
              name={node.name}
              status="ok"
              health={100}
              cpu={node.cpuPct || 10}
              mem={node.memPct || 25}
              clients={node.connections || 0}
              region="Global"
              onClick={() => onNodeClick?.(node.id)}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

function TimelinePanel({ data }: { data: DashboardTimelineData }) {
  if (!data?.events) return null;
  return (
    <div className="flex flex-col gap-4 bg-bg-card border border-border rounded-xl p-5 shadow-sm min-h-[calc(100vh-10rem)]">
      <SectionHeader title="Recent Events" trailing={<Badge variant="accent">live</Badge>} />
      <Timeline
        events={data.events.map((e) => ({
          status: e.status === "info" ? ("ok" as const) : e.status,
          time: e.time,
          message: e.message,
        }))}
      />
    </div>
  );
}

const emptyFormData: ClientFormData = {
  name: "",
  userAdTag: "",
  expirationRfc3339: "",
  maxTcpConns: 0,
  maxUniqueIps: 0,
  dataQuotaBytes: 0,
};

export function DashboardPage({
  overview,
  timeline,
  onNodeClick,
  onCreate,
  createLoading,
  createError,
  pendingDiscoveredCount,
  onDiscoveredClick,
}: DashboardPageProps) {
  const [createOpen, setCreateOpen] = useState(false);
  const [createData, setCreateData] = useState<ClientFormData>({ ...emptyFormData });

  return (
    <>
      <PageHeader
        title="Dashboard"
        subtitle="System overview"
        trailing={
          onCreate ? (
            <Button
              size="sm"
              onClick={() => {
                setCreateData({ ...emptyFormData });
                setCreateOpen(true);
              }}
            >
              Add Client
            </Button>
          ) : undefined
        }
      />
      <div className="px-4 md:px-8 pb-8">
        {!!pendingDiscoveredCount && (
          <div className="mb-5">
            <DiscoveredClientsBanner count={pendingDiscoveredCount} onClick={onDiscoveredClick} />
          </div>
        )}
        {/* Mobile: swipe tabs */}
        <div className="md:hidden">
          <SwipeTabView
            tabs={[
              {
                id: "overview",
                label: "Overview",
                content: (
                  <div className="pt-4">
                    <OverviewPanel data={overview} onNodeClick={onNodeClick} />
                  </div>
                ),
              },
              {
                id: "timeline",
                label: "Timeline",
                content: (
                  <div className="pt-4">
                    <TimelinePanel data={timeline} />
                  </div>
                ),
              },
            ]}
          />
        </div>

        {/* Desktop: two columns */}
        <div className="hidden md:grid md:grid-cols-[1fr_320px] gap-6 mt-6">
          <OverviewPanel data={overview} onNodeClick={onNodeClick} />
          <aside className="flex flex-col gap-6">
            <TimelinePanel data={timeline} />
          </aside>
        </div>
      </div>

      {onCreate && (
        <Sheet
          open={createOpen}
          onOpenChange={(open) => {
            if (!open) setCreateOpen(false);
          }}
        >
          <SheetContent side="bottom">
            <SheetBody>
              <ClientFormSheet
                mode="create"
                data={createData}
                onChange={setCreateData}
                onSubmit={async () => {
                  await onCreate(createData);
                  if (!createError) setCreateOpen(false);
                }}
                onCancel={() => setCreateOpen(false)}
                loading={createLoading}
                error={createError}
              />
            </SheetBody>
          </SheetContent>
        </Sheet>
      )}
    </>
  );
}
