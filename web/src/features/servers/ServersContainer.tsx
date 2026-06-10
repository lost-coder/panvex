import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation } from "@tanstack/react-query";
import { type BulkServerAction, type ViewMode, Button, EmptyState } from "@/ui";
import { ServersPage } from "@/features/servers/ServersPage";
import { SkeletonRows } from "@/ui";
import { useServersList } from "./hooks/useServersList";
import { useFleetGroups } from "./hooks/useFleetGroups";
import { useViewMode } from "@/shared/hooks/useViewMode";
import { useUpdates } from "@/shared/hooks/useUpdates";
import { useNavigate } from "@tanstack/react-router";
import { ErrorState } from "@/components/ErrorState";
import { useUrlSearchState } from "@/shared/hooks/useUrlSearchState";
import { useWsUpdateFlash } from "@/shared/hooks/useWsUpdateFlash";
import { apiClient } from "@/shared/api/api";
import { useToast } from "@/app/providers/ToastProvider";

const BULK_ACTION_MAP: Record<BulkServerAction, string> = {
  reload: "runtime.reload",
  selfUpdate: "agent.self-update",
};

export function ServersContainer() {
  const { t } = useTranslation("servers");
  const { servers, agentVersions, isLoading, error } = useServersList();
  const { fleetGroups } = useFleetGroups();
  const { resolveMode, setMode } = useViewMode("servers");
  const { query: updatesQuery } = useUpdates();
  const latestAgentVersion = updatesQuery.data?.state.latest_agent_version;
  const navigate = useNavigate();
  const flashing = useWsUpdateFlash();
  const toast = useToast();

  const [bulkError, setBulkError] = useState<string | undefined>();
  const bulkMutation = useMutation({
    mutationFn: async ({
      action,
      agentIds,
    }: {
      action: BulkServerAction;
      agentIds: string[];
    }) => {
      await apiClient.createJob({
        action: BULK_ACTION_MAP[action],
        target_agent_ids: agentIds,
        idempotency_key: crypto.randomUUID(),
        ttl_seconds: 300,
      });
    },
    onError: (err: unknown) =>
      setBulkError(err instanceof Error ? err.message : t("error.bulkActionFailed")),
    onSuccess: (_data, vars) => {
      setBulkError(undefined);
      // Audit E5: bulk actions were fire-and-forget — confirm the enqueue
      // and hand the operator a one-tap path to watch the rollout.
      toast.withAction(
        "success",
        t("bulk.queued", { count: vars.agentIds.length }),
        {
          label: t("bulk.viewActivity"),
          onClick: () => void navigate({ to: "/activity" }),
        },
      );
    },
  });

  // P2-UX-05: persist viewMode in the URL so a shared link lands in the
  // same card/list mode. localStorage still holds the user's long-term
  // preference via useViewMode.
  const [viewParam, setViewParam] = useUrlSearchState("view", "");

  if (isLoading) {
    return (
      <div className="px-4 md:px-8 py-8">
        <SkeletonRows count={6} />
      </div>
    );
  }

  if (error) {
    return (
      <ErrorState
        title={t("error.loadFleet")}
        description={error.message || t("error.fallbackDescription")}
        onRetry={() => globalThis.location.reload()}
      />
    );
  }

  // 2.5: empty state for first-time operators. An action button is
  // included because adding a node requires navigating to a dedicated
  // wizard — making it discoverable here halves the clicks.
  if (servers.length === 0) {
    return (
      <div className="p-6">
        <EmptyState
          icon="🖥️"
          title={t("empty.title")}
          description={t("empty.description")}
          action={
            <Button onClick={() => navigate({ to: "/servers/add" })}>
              {t("empty.addServer")}
            </Button>
          }
        />
      </div>
    );
  }

  // Enrich servers with update availability when latest version is known
  const enrichedServers = latestAgentVersion
    ? servers.map((s) => ({
        ...s,
        updateAvailable:
          !!agentVersions[s.id] && agentVersions[s.id] !== latestAgentVersion,
      }))
    : servers;

  const urlView = viewParam === "cards" || viewParam === "list" ? (viewParam as ViewMode) : undefined;
  const effectiveMode = urlView ?? resolveMode(servers.length);

  return (
    <div className={flashing ? "transition-[box-shadow] duration-300 ring-2 ring-accent/20 rounded" : undefined}>
      <ServersPage
        servers={enrichedServers}
        viewMode={effectiveMode}
        autoThreshold={10}
        fleetGroups={fleetGroups.map((g) => ({ id: g.id, label: g.label || g.name || g.id, agentCount: g.agent_count }))}
        onViewModeChange={(m) => {
          setMode(m);
          setViewParam(m);
        }}
        onServerClick={(id) => navigate({ to: "/servers/$serverId", params: { serverId: id } })}
        onAddServer={() => navigate({ to: "/servers/add" })}
        onManageTokens={() => navigate({ to: "/servers/enrollment" })}
        onBulkAction={(action, agentIds) =>
          bulkMutation.mutateAsync({ action, agentIds })
        }
        bulkError={bulkError}
        bulkPending={bulkMutation.isPending}
      />
    </div>
  );
}
