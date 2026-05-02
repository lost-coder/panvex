// Phase-7 redesign: handoff-style fleet overview. KPIs render as 4 dense
// tiles, the server list unifies attention + healthy nodes with a visual
// divider, and the header carries a live-refresh indicator.
import { ClientFormSheet } from "@/features/clients/ClientFormSheet";
import { DiscoveredClientsBanner } from "@/features/clients/DiscoveredClientsBanner";
import { useState } from "react";
import {
  Button,
  PageHeader,
  Sheet,
  SheetBody,
  SheetContent,
  StatusDot,
  SwipeTabView,
  type ClientFormData,
  type DashboardPageProps,
} from "@/ui";
import { KpiStrip } from "./ui/KpiStrip";
import { FleetPanel } from "./ui/FleetPanel";
import { TimelinePanel } from "./ui/TimelinePanel";

const emptyFormData: ClientFormData = {
  name: "",
  userAdTag: "",
  userAdTagAuto: true,
  expirationRfc3339: "",
  maxTcpConns: 0,
  maxUniqueIps: 0,
  dataQuotaBytes: 0,
  fleetGroupIds: [],
  agentIds: [],
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
  onViewAllServers,
}: Readonly<DashboardPageProps>) {
  const [createOpen, setCreateOpen] = useState(false);
  const [createData, setCreateData] = useState<ClientFormData>({ ...emptyFormData });

  return (
    <>
      <PageHeader
        title="Dashboard"
        subtitle="Realtime fleet overview · MTProto proxy operations"
        trailing={
          <div className="flex items-center gap-3">
            {/* Phase-7 live indicator: mirrors the 15s refetch interval of
                useDashboardData so the operator can see that the page is
                pulling fresh telemetry. */}
            <span
              aria-live="polite"
              className="hidden sm:flex items-center gap-1.5 text-[11px] font-mono text-fg-muted"
            >
              <StatusDot status="ok" className="animate-pulse" />
              live · 15s refresh
            </span>
            {onCreate && (
              <Button
                size="sm"
                onClick={() => {
                  setCreateData({ ...emptyFormData });
                  setCreateOpen(true);
                }}
              >
                Add Client
              </Button>
            )}
          </div>
        }
      />
      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        {/* Phase-7 layout: banner + KPI tiles span full width. The Active
            Alerts block was removed — FleetList already surfaces problem
            nodes in a "Needs attention" section with the same severity
            signal, so a separate alerts card would be pure duplication. */}
        {!!pendingDiscoveredCount && (
          <DiscoveredClientsBanner count={pendingDiscoveredCount} onClick={onDiscoveredClick} />
        )}
        <KpiStrip kpis={overview.kpis} />

        {/* Mobile: swipe tabs between fleet and activity to avoid a long scroll. */}
        <div className="md:hidden">
          <SwipeTabView
            tabs={[
              {
                id: "fleet",
                label: "Fleet",
                content: (
                  <div className="pt-4">
                    <FleetPanel
                      data={overview}
                      onNodeClick={onNodeClick}
                      onViewAll={onViewAllServers}
                    />
                  </div>
                ),
              },
              {
                id: "timeline",
                label: "Activity",
                content: (
                  <div className="pt-4">
                    <TimelinePanel data={timeline} />
                  </div>
                ),
              },
            ]}
          />
        </div>

        {/* Desktop: fleet column gets ~2.2x the width of the activity column so
            the per-node rows have room for CPU/MEM load bars + traffic. */}
        <div className="hidden md:grid md:grid-cols-[minmax(0,2.2fr)_minmax(280px,1fr)] gap-6 items-start">
          {/* items-start prevents the grid from stretching the Fleet card
              to match a tall Recent Events column. */}
          <FleetPanel
            data={overview}
            onNodeClick={onNodeClick}
            onViewAll={onViewAllServers}
          />
          <TimelinePanel data={timeline} />
        </div>
      </div>

      {onCreate && (
        <Sheet
          open={createOpen}
          onOpenChange={(open) => {
            if (!open) setCreateOpen(false);
          }}
        >
          <SheetContent side="bottom" title="Add client">
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
