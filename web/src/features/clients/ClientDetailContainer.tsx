import { useCallback, useRef, useState, useEffect, useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Spinner, formatBytes } from "@/ui";
import { ClientDetailPage } from "@/features/clients/ClientDetailPage";
import { useClientDetail } from "./hooks/useClientDetail";
import { useClientMutations } from "./hooks/useClientMutations";
import { useClientIPHistory } from "./hooks/useClientIPHistory";
import { useResetQuota } from "./hooks/useResetQuota";
import { clientsKeys } from "@/features/clients/queryKeys";
import { useFleetGroups } from "@/features/servers/hooks/useFleetGroups";
import { agentsKeys } from "@/features/servers/queryKeys";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useAuth } from "@/app/providers/AuthProvider";
import { useConfirm } from "@/app/providers/ConfirmProvider";
import { useToast } from "@/app/providers/ToastProvider";
import { apiClient } from "@/shared/api/api";
import { buildClientInput } from "@/shared/api/transforms/clients";

export function ClientDetailContainer() {
  const { t } = useTranslation("clients");
  const { clientId } = useParams({ strict: false });
  const { client, raw, isLoading, error } = useClientDetail(clientId ?? "");
  const { editMutation, rotateMutation, redeployMutation, deleteMutation } = useClientMutations(clientId ?? "", raw);
  const { ips, totalUnique } = useClientIPHistory(clientId ?? "");
  const navigate = useNavigate();
  const confirm = useConfirm();
  const toast = useToast();
  const { user } = useAuth();
  const qc = useQueryClient();
  const [secretPending, setSecretPending] = useState(false);

  // Reset-quota Phase 2: only operators+admins can fire the reset.
  // Viewers see the cell but the button is hidden — the backend also
  // denies them with 403, but hiding the affordance keeps the UI
  // honest about what the user can actually do.
  const canResetQuota = user?.role === "operator" || user?.role === "admin";

  // useResetQuota owns the job-poll state machine and per-row outcome
  // map; the container provides the toast callback so i18n strings
  // stay co-located with the page.
  const agentLabelLookupRef = useRef<Record<string, string>>({});
  const onResetSuccessToast = useCallback(
    (scope: "agent" | "all", payload: { agentId?: string; count?: number }) => {
      if (scope === "agent") {
        const id = payload.agentId ?? "";
        const label = agentLabelLookupRef.current[id] ?? id;
        toast.success(t("detail.quota.resetSuccess", { agent: label }));
        return;
      }
      toast.success(t("detail.quota.resetSuccessAll", { count: payload.count ?? 0 }));
    },
    [toast, t, agentLabelLookupRef],
  );
  const {
    rowStates,
    fanOutPending,
    resetOnAgent,
    resetEverywhere,
    clearRow,
  } = useResetQuota(clientId ?? "", onResetSuccessToast);

  // Toggling `enabled` is a PUT /clients/:id with the full ClientInput,
  // so we fan it out here rather than adding another branch to
  // useClientMutations.
  // Join agent_id → node_name client-side for the Deployments & Links
  // card. Cached alongside the agents list used elsewhere so the request
  // is shared when an operator bounces between /servers and a client
  // detail page. See backend-followup #5.
  const agentsQuery = useQuery({
    queryKey: agentsKeys.list(),
    queryFn: () => apiClient.agents(),
    staleTime: 30_000,
  });
  const agentLabels = useMemo(() => {
    const map: Record<string, string> = {};
    for (const a of agentsQuery.data ?? []) {
      map[a.id] = a.node_name || a.id;
    }
    return map;
  }, [agentsQuery.data]);
  // Keep the latest label lookup on a ref so the reset-success toast
  // callback can resolve agent_id → node_name without re-binding the
  // callback (and therefore the hook) on every render. useEffect keeps
  // the lint rule happy ("Cannot access refs during render"); the
  // toast callback only reads the ref inside an async handler so the
  // one-render lag is invisible to the user.
  useEffect(() => {
    agentLabelLookupRef.current = agentLabels;
  }, [agentLabels]);
  const agentOptions = useMemo(
    () =>
      (agentsQuery.data ?? []).map((a) => ({
        id: a.id,
        nodeName: a.node_name || a.id,
        fleetGroupId: a.fleet_group_id,
        online: a.presence_state === "online",
      })),
    [agentsQuery.data],
  );
  const { fleetGroups } = useFleetGroups();
  const fleetGroupOptions = useMemo(
    () => fleetGroups.map((g) => ({ id: g.id, label: g.label || g.name || g.id, agentCount: g.agent_count })),
    [fleetGroups],
  );

  const toggleEnabledMutation = useMutation({
    mutationFn: async (nextEnabled: boolean) => {
      if (!raw) throw new Error("Client data not loaded");
      // Toggle keeps every deployment target the client already had —
      // flipping `enabled` must not inadvertently wipe assignments.
      const payload = buildClientInput(
        {
          name: raw.name,
          userAdTag: raw.user_ad_tag,
          // Toggle is a pure enable/disable — keep the stored ad tag
          // verbatim, don't trigger a regeneration cycle.
          userAdTagAuto: false,
          expirationRfc3339: raw.expiration_rfc3339,
          maxTcpConns: raw.max_tcp_conns,
          maxUniqueIps: raw.max_unique_ips,
          dataQuotaBytes: raw.data_quota_bytes,
          fleetGroupIds: raw.fleet_group_ids ?? [],
          agentIds: raw.agent_ids ?? [],
        },
        { ...raw, enabled: nextEnabled },
      );
      return apiClient.updateClient(raw.id, payload);
    },
    onSuccess: () => {
      if (clientId) void qc.invalidateQueries({ queryKey: clientsKeys.detail(clientId) });
      void qc.invalidateQueries({ queryKey: clientsKeys.all });
    },
  });

  // Reset pending state when fresh server data arrives after rotation.
  // This is a legitimate "sync local state with external system" effect
  // (React Query refetch finishing); the guard makes it idempotent so
  // the linter's cascade warning doesn't apply.
  useEffect(() => {
    if (raw && secretPending) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setSecretPending(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [raw]);

  if (error) {
    // Without this branch any failure (network, schema-mismatch, 404)
    // left the page hanging on the spinner forever — the parent
    // ErrorBoundary never sees React Query errors and the prior copy
    // of this container only checked `isLoading || !client`.
    return (
      <div className="flex flex-col items-center justify-center h-64 gap-2 text-center px-6">
        <p className="text-sm text-fg">{t("detail.loadError")}</p>
        <p className="text-xs text-fg-muted max-w-md">{error.message}</p>
        <button
          type="button"
          onClick={() => {
            if (clientId) void qc.invalidateQueries({ queryKey: clientsKeys.detail(clientId) });
          }}
          className="mt-2 px-3 py-1 text-xs font-mono rounded-xs border border-border-hi hover:border-accent"
        >
          {t("detail.retry")}
        </button>
      </div>
    );
  }

  if (isLoading || !client) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  return (
    <ClientDetailPage
      client={client}
      onBack={() => navigate({ to: "/clients" })}
      onEdit={async (data) => {
        await editMutation.mutateAsync(data);
      }}
      editLoading={editMutation.isPending}
      editError={editMutation.error?.message}
      fleetGroups={fleetGroupOptions}
      agents={agentOptions}
      // P2-UX-04: rotating a secret invalidates all client devices — gate
      // it behind a confirm dialog so an accidental click doesn't lock users out.
      onRotateSecret={async () => {
        const ok = await confirm({
          title: t("detail.confirm.rotateTitle"),
          body: t("detail.confirm.rotateBody", { name: client.name }),
          confirmLabel: t("detail.confirm.rotateConfirm"),
          variant: "danger",
        });
        if (!ok) return;
        await rotateMutation.mutateAsync();
        setSecretPending(true);
      }}
      secretRotating={rotateMutation.isPending}
      secretPendingRedeploy={secretPending}
      onRedeploy={async () => {
        await redeployMutation.mutateAsync();
      }}
      redeploying={redeployMutation.isPending}
      onDisable={async () => {
        const next = !(raw?.enabled ?? true);
        const ok = await confirm({
          title: next ? t("detail.confirm.enableTitle") : t("detail.confirm.disableTitle"),
          body: next
            ? t("detail.confirm.enableBody", { name: client.name })
            : t("detail.confirm.disableBody", { name: client.name }),
          confirmLabel: next ? t("detail.confirm.enableConfirm") : t("detail.confirm.disableConfirm"),
          variant: next ? "default" : "danger",
        });
        if (!ok) return;
        await toggleEnabledMutation.mutateAsync(next);
      }}
      // P2-UX-04: destructive and irreversible — the confirm dialog is the
      // last safety net before the client row disappears fleet-wide.
      onDelete={async () => {
        const ok = await confirm({
          title: t("detail.confirm.deleteTitle"),
          body: t("detail.confirm.deleteBody", { name: client.name }),
          confirmLabel: t("detail.confirm.deleteConfirm"),
          variant: "danger",
          // UX-05: irreversible and fleet-wide — force the operator to
          // type the client name so a misclick can't wipe a tenant.
          requireTypeMatch: client.name,
        });
        if (!ok) return;
        await deleteMutation.mutateAsync();
        void navigate({ to: "/clients" });
      }}
      ipHistory={ips.length > 0 ? { ips, totalUnique } : undefined}
      agentLabels={agentLabels}
      onResetQuota={
        canResetQuota
          ? async (agentId: string) => {
              const deployment = client.deployments.find(
                (d) => d.agentId === agentId,
              );
              const agentLabel = agentLabels[agentId] ?? agentId;
              const usedLabel = formatBytes(deployment?.quotaUsedBytes ?? 0);
              const ok = await confirm({
                title: t("detail.quota.resetConfirmTitle"),
                body: t("detail.quota.resetConfirmBody", {
                  client: client.name,
                  agent: agentLabel,
                  used: usedLabel,
                }),
                confirmLabel: t("detail.quota.resetConfirmAction"),
                cancelLabel: t("detail.quota.resetCancel"),
              });
              if (!ok) return;
              try {
                await resetOnAgent(agentId);
              } catch (err) {
                // Mutation onError already populated rowStates with
                // the failure — catch here just suppresses the
                // unhandled-promise warning when the operator backs
                // out via a follow-up confirm.
                if (err instanceof Error) {
                  // dev-only triage; production drops via
                  // notifyMutationError elsewhere
                  console.debug("resetOnAgent failed", err.message);
                }
              }
            }
          : undefined
      }
      resetStates={rowStates}
      onDismissResetState={clearRow}
      onResetQuotaEverywhere={
        canResetQuota
          ? async () => {
              const agentIds = client.deployments.map((d) => d.agentId);
              if (agentIds.length === 0) return;
              const totalUsed = client.deployments.reduce(
                (sum, d) => sum + (d.quotaUsedBytes || 0),
                0,
              );
              const ok = await confirm({
                title: t("detail.quota.resetConfirmTitle"),
                body: t("detail.quota.resetConfirmBodyAll", {
                  client: client.name,
                  count: agentIds.length,
                  used: formatBytes(totalUsed),
                }),
                confirmLabel: t("detail.quota.resetConfirmAction"),
                cancelLabel: t("detail.quota.resetCancel"),
              });
              if (!ok) return;
              try {
                await resetEverywhere(agentIds);
              } catch (err) {
                if (err instanceof Error) {
                  console.debug("resetEverywhere failed", err.message);
                }
              }
            }
          : undefined
      }
      resetEverywherePending={fanOutPending}
    />
  );
}
