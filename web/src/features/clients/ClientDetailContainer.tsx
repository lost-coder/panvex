import { useState, useEffect, useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Spinner } from "@/ui";
import { ClientDetailPage } from "@/features/clients/ClientDetailPage";
import { useClientDetail } from "./hooks/useClientDetail";
import { useClientMutations } from "./hooks/useClientMutations";
import { useClientIPHistory } from "./hooks/useClientIPHistory";
import { useFleetGroups } from "@/features/servers/hooks/useFleetGroups";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useConfirm } from "@/app/providers/ConfirmProvider";
import { apiClient } from "@/shared/api/api";
import { buildClientInput } from "@/shared/api/transforms/clients";

export function ClientDetailContainer() {
  const { clientId } = useParams({ strict: false });
  const { client, raw, isLoading } = useClientDetail(clientId ?? "");
  const { editMutation, rotateMutation, deleteMutation } = useClientMutations(clientId ?? "", raw);
  const { ips, totalUnique } = useClientIPHistory(clientId ?? "");
  const navigate = useNavigate();
  const confirm = useConfirm();
  const qc = useQueryClient();
  const [secretPending, setSecretPending] = useState(false);

  // Toggling `enabled` is a PUT /clients/:id with the full ClientInput,
  // so we fan it out here rather than adding another branch to
  // useClientMutations.
  // Join agent_id → node_name client-side for the Deployments & Links
  // card. Cached alongside the agents list used elsewhere so the request
  // is shared when an operator bounces between /servers and a client
  // detail page. See backend-followup #5.
  const agentsQuery = useQuery({
    queryKey: ["agents"],
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
      if (clientId) qc.invalidateQueries({ queryKey: ["client", clientId] });
      qc.invalidateQueries({ queryKey: ["clients"] });
    },
  });

  // Reset pending state when fresh server data arrives after rotation
  useEffect(() => {
    if (raw && secretPending) {
      setSecretPending(false);
    }
  }, [raw]);

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
          title: "Rotate client secret?",
          body: `Existing connection links for "${client.name}" will stop working until operators redeploy them.`,
          confirmLabel: "Rotate secret",
          variant: "danger",
        });
        if (!ok) return;
        await rotateMutation.mutateAsync();
        setSecretPending(true);
      }}
      secretRotating={rotateMutation.isPending}
      secretPendingRedeploy={secretPending}
      onDisable={async () => {
        const next = !(raw?.enabled ?? true);
        const ok = await confirm({
          title: next ? "Enable client?" : "Disable client?",
          body: next
            ? `"${client.name}" will start accepting connections again after agents re-apply.`
            : `"${client.name}" will stop accepting new connections on every node until re-enabled.`,
          confirmLabel: next ? "Enable" : "Disable",
          variant: next ? "default" : "danger",
        });
        if (!ok) return;
        await toggleEnabledMutation.mutateAsync(next);
      }}
      // P2-UX-04: destructive and irreversible — the confirm dialog is the
      // last safety net before the client row disappears fleet-wide.
      onDelete={async () => {
        const ok = await confirm({
          title: "Delete client?",
          body: `This removes "${client.name}" from every node and revokes all its connection links.`,
          confirmLabel: "Delete client",
          variant: "danger",
          // UX-05: irreversible and fleet-wide — force the operator to
          // type the client name so a misclick can't wipe a tenant.
          requireTypeMatch: client.name,
        });
        if (!ok) return;
        await deleteMutation.mutateAsync();
        navigate({ to: "/clients" });
      }}
      ipHistory={ips.length > 0 ? { ips, totalUnique } : undefined}
      agentLabels={agentLabels}
    />
  );
}
