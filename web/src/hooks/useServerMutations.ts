import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api";

export function useServerMutations(serverId: string) {
  const qc = useQueryClient();

  const invalidateServer = () => {
    qc.invalidateQueries({ queryKey: ["telemetry", "server", serverId] });
    qc.invalidateQueries({ queryKey: ["telemetry", "servers"] });
  };

  const allowCertRecoveryMutation = useMutation({
    mutationFn: () => apiClient.allowAgentCertificateRecovery(serverId),
    onSuccess: invalidateServer,
    onError: (err) => console.error("Failed to allow certificate recovery:", err),
  });

  const revokeCertRecoveryMutation = useMutation({
    mutationFn: () => apiClient.revokeAgentCertificateRecovery(serverId),
    onSuccess: invalidateServer,
    onError: (err) => console.error("Failed to revoke certificate recovery:", err),
  });

  const boostDetailMutation = useMutation({
    mutationFn: () => apiClient.activateTelemetryDetailBoost(serverId),
    onSuccess: invalidateServer,
    onError: (err) => console.error("Failed to activate detail boost:", err),
  });

  const renameMutation = useMutation({
    mutationFn: (nodeName: string) => apiClient.renameAgent(serverId, nodeName),
    onSuccess: invalidateServer,
    onError: (err) => console.error("Failed to rename agent:", err),
  });

  const deregisterMutation = useMutation({
    mutationFn: () => apiClient.deregisterAgent(serverId),
    onError: (err) => console.error("Failed to deregister agent:", err),
  });

  return {
    allowCertRecoveryMutation,
    revokeCertRecoveryMutation,
    boostDetailMutation,
    renameMutation,
    deregisterMutation,
  };
}
