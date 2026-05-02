import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/shared/api/api";
import { notifyMutationError } from "@/shared/api/http";

export function useProfileTotp() {
  const qc = useQueryClient();

  const invalidateProfile = () => {
    qc.invalidateQueries({ queryKey: ["me"] });
  };

  const setupMutation = useMutation({
    mutationFn: () => apiClient.startTotpSetup(),
    onError: (err) => notifyMutationError("auth", "totp.setup", err),
  });

  const enableMutation = useMutation({
    mutationFn: (payload: { password: string; totp_code: string }) =>
      apiClient.enableTotp(payload),
    onSuccess: () => {
      invalidateProfile();
      // Clear TOTP secret from mutation cache after successful enable
      setupMutation.reset();
    },
    onError: (err) => notifyMutationError("auth", "totp.enable", err),
  });

  const disableMutation = useMutation({
    mutationFn: (payload: { password: string; totp_code: string }) =>
      apiClient.disableTotp(payload),
    onSuccess: invalidateProfile,
    onError: (err) => notifyMutationError("auth", "totp.disable", err),
  });

  return { setupMutation, enableMutation, disableMutation };
}
