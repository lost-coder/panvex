import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/shared/api/api";
import { notifyMutationError } from "@/shared/api/http";

export function useServerMutations(serverId: string) {
  const qc = useQueryClient();

  const invalidateServer = () => {
    qc.invalidateQueries({ queryKey: ["telemetry", "server", serverId] });
    qc.invalidateQueries({ queryKey: ["telemetry", "servers"] });
  };

  const allowCertRecoveryMutation = useMutation({
    mutationFn: () => apiClient.allowAgentCertificateRecovery(serverId),
    onSuccess: invalidateServer,
    onError: (err) => notifyMutationError("servers", "cert-recovery.allow", err),
  });

  const revokeCertRecoveryMutation = useMutation({
    mutationFn: () => apiClient.revokeAgentCertificateRecovery(serverId),
    onSuccess: invalidateServer,
    onError: (err) => notifyMutationError("servers", "cert-recovery.revoke", err),
  });

  const boostDetailMutation = useMutation({
    mutationFn: () => apiClient.activateTelemetryDetailBoost(serverId),
    onSuccess: invalidateServer,
    onError: (err) => notifyMutationError("servers", "detail-boost.activate", err),
  });

  const renameMutation = useMutation({
    mutationFn: (nodeName: string) => apiClient.renameAgent(serverId, nodeName),
    onSuccess: invalidateServer,
    onError: (err) => notifyMutationError("servers", "agent.rename", err),
  });

  const updateFleetGroupMutation = useMutation({
    mutationFn: (fleetGroupId: string) => apiClient.updateAgentFleetGroup(serverId, fleetGroupId),
    onSuccess: () => {
      invalidateServer();
      // Fleet-group member counts on the groups list change too.
      qc.invalidateQueries({ queryKey: ["fleet-groups"] });
    },
    onError: (err) => notifyMutationError("servers", "agent.update-fleet-group", err),
  });

  const deregisterMutation = useMutation({
    mutationFn: () => apiClient.deregisterAgent(serverId),
    onSuccess: () => {
      invalidateServer();
      qc.invalidateQueries({ queryKey: ["agents"] });
      qc.invalidateQueries({ queryKey: ["control-room"] });
    },
    onError: (err) => notifyMutationError("servers", "agent.deregister", err),
  });

  return {
    allowCertRecoveryMutation,
    revokeCertRecoveryMutation,
    boostDetailMutation,
    renameMutation,
    updateFleetGroupMutation,
    deregisterMutation,
  };
}
