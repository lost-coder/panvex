import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api";

export function useProfileTotp() {
  const qc = useQueryClient();

  const invalidateProfile = () => {
    qc.invalidateQueries({ queryKey: ["me"] });
  };

  const setupMutation = useMutation({
    mutationFn: () => apiClient.startTotpSetup(),
    onError: (err) => console.error("Failed to start TOTP setup:", err),
  });

  const enableMutation = useMutation({
    mutationFn: (payload: { password: string; totp_code: string }) =>
      apiClient.enableTotp(payload),
    onSuccess: invalidateProfile,
    onError: (err) => console.error("Failed to enable TOTP:", err),
  });

  const disableMutation = useMutation({
    mutationFn: (payload: { password: string; totp_code: string }) =>
      apiClient.disableTotp(payload),
    onSuccess: invalidateProfile,
    onError: (err) => console.error("Failed to disable TOTP:", err),
  });

  return { setupMutation, enableMutation, disableMutation };
}
