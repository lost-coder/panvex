import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api";

export function useServerMutations(serverId: string) {
  const qc = useQueryClient();

  const invalidateServer = () => {
    qc.invalidateQueries({ queryKey: ["telemetry", "server", serverId] });
  };

  const allowCertRecoveryMutation = useMutation({
    mutationFn: () => apiClient.allowAgentCertificateRecovery(serverId),
    onSuccess: invalidateServer,
  });

  const revokeCertRecoveryMutation = useMutation({
    mutationFn: () => apiClient.revokeAgentCertificateRecovery(serverId),
    onSuccess: invalidateServer,
  });

  const boostDetailMutation = useMutation({
    mutationFn: () => apiClient.activateTelemetryDetailBoost(serverId),
    onSuccess: invalidateServer,
  });

  return {
    allowCertRecoveryMutation,
    revokeCertRecoveryMutation,
    boostDetailMutation,
  };
}
